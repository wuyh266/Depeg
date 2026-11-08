package monitor

import (
	"context"
	"encoding/json"
	"math/big"
	"regexp"
	"time"

	"depeg.local/platform/internal/domain"
	"depeg.local/platform/internal/domain/market"
	entity "depeg.local/platform/internal/domain/monitor"
	"depeg.local/platform/internal/identity"
	"depeg.local/platform/internal/port"
)

type service struct {
	repository port.MonitorRepository
	market     port.MarketService
}

func New(r port.MonitorRepository, m port.MarketService) port.MonitorService {
	return &service{repository: r, market: m}
}

var decimal = regexp.MustCompile(`^(?:0|[1-9][0-9]{0,19})(?:\.[0-9]{1,18})?$`)

func positive(raw string) (*big.Rat, bool) {
	if !decimal.MatchString(raw) {
		return nil, false
	}
	v, ok := new(big.Rat).SetString(raw)
	return v, ok && v.Sign() > 0
}

// Evaluate compares rational decimals before rounding the display value.
func Evaluate(price, anchor string, warning, critical int) (string, string, error) {
	p, ok := positive(price)
	if !ok {
		return "", "", domain.ErrInvalid
	}
	a, ok := positive(anchor)
	if !ok || warning < 1 || critical <= warning || critical > 100000 {
		return "", "", domain.ErrInvalid
	}
	d := new(big.Rat).Mul(new(big.Rat).Quo(new(big.Rat).Sub(p, a), a), big.NewRat(10000, 1))
	abs := new(big.Rat).Abs(d)
	status := "stable"
	if abs.Cmp(big.NewRat(int64(critical), 1)) >= 0 {
		status = "critical"
	} else if abs.Cmp(big.NewRat(int64(warning), 1)) >= 0 {
		status = "warning"
	}
	return d.FloatString(6), status, nil
}

func sameStream(r entity.Rule, s entity.Stream) bool {
	return r.Kind == s.Kind && r.Source == s.Source && r.IntervalSeconds == s.IntervalSeconds
}

func (s *service) Overview(ctx context.Context, user string) (entity.Overview, error) {
	if user == "" {
		return entity.Overview{}, domain.ErrUnauthorized
	}
	assets, err := s.repository.Catalog(ctx)
	if err != nil {
		return entity.Overview{}, err
	}
	rules, err := s.repository.Rules(ctx, user)
	if err != nil {
		return entity.Overview{}, err
	}
	byAsset := map[string]entity.Rule{}
	for _, r := range rules {
		byAsset[r.AssetID] = r
	}
	now := time.Now().UTC()
	for i := range assets {
		a := &assets[i]
		a.Status = "unknown"
		a.Freshness = "missing"
		a.AnchorOrigin = "unknown"
		a.WarningBPS = 50
		a.CriticalBPS = 100
		a.AnchorPrice = a.Asset.AnchorPrice
		if a.AnchorPrice != nil {
			a.AnchorOrigin = "asset"
		}
		if len(a.Streams) > 0 {
			a.Selected = &a.Streams[0]
		}
		if r, ok := byAsset[a.Asset.ID]; ok {
			rule := r
			a.Rule = &rule
			a.AnchorPrice = &rule.AnchorPrice
			a.AnchorOrigin = "account"
			a.WarningBPS = r.WarningBPS
			a.CriticalBPS = r.CriticalBPS
			a.Selected = nil
			for j := range a.Streams {
				if sameStream(r, a.Streams[j]) {
					a.Selected = &a.Streams[j]
					break
				}
			}
		}
		if a.Selected == nil {
			continue
		}
		if a.Rule == nil && a.Selected.Kind == "daily" && a.Asset.QuoteCurrency != "USD" {
			a.AnchorPrice = nil
			a.AnchorOrigin = "unknown"
		}
		p := a.Selected.Latest
		maxAge := 3 * time.Duration(a.Selected.IntervalSeconds) * time.Second
		a.Freshness = "recent"
		if p.ObservedAt.Before(now.Add(-maxAge)) {
			a.Freshness = "historical"
		}
		if p.ObservedAt.After(now.Add(5 * time.Second)) {
			a.Freshness = "future"
		}
		if a.AnchorPrice != nil {
			d, status, err := Evaluate(p.Price, *a.AnchorPrice, a.WarningBPS, a.CriticalBPS)
			if err == nil {
				a.DeviationBPS = &d
				a.Status = status
			}
		}
	}
	return entity.Overview{Assets: assets, AsOf: now}, nil
}

func (s *service) SaveRule(ctx context.Context, user string, r entity.Rule) (entity.Rule, error) {
	if user == "" {
		return entity.Rule{}, domain.ErrUnauthorized
	}
	if _, ok := positive(r.AnchorPrice); !ok || r.WarningBPS < 1 || r.CriticalBPS <= r.WarningBPS || r.CriticalBPS > 100000 {
		return entity.Rule{}, domain.ErrInvalid
	}
	assets, err := s.repository.Catalog(ctx)
	if err != nil {
		return entity.Rule{}, err
	}
	found := false
	for _, a := range assets {
		if a.Asset.ID == r.AssetID {
			for _, stream := range a.Streams {
				if sameStream(r, stream) {
					found = true
				}
			}
		}
	}
	if !found {
		return entity.Rule{}, domain.ErrInvalid
	}
	// Canonicalize equal anchors so 1 and 1.00 use the same event identity.
	value, _ := positive(r.AnchorPrice)
	r.AnchorPrice = value.FloatString(18)
	r.UserID = user
	r.Version = ""
	r.UpdatedAt = time.Time{}
	canonical, _ := json.Marshal(r)
	r.Version = identity.Digest(user + "|" + string(canonical))
	r.UpdatedAt = time.Now().UTC()
	if err := s.repository.SaveRule(ctx, r); err != nil {
		return entity.Rule{}, err
	}
	return r, nil
}

func (s *service) Replay(ctx context.Context, user string, q entity.ReplayQuery) (entity.ReplayResult, error) {
	result := entity.ReplayResult{From: q.From.UTC(), To: q.To.UTC()}
	if user == "" {
		return result, domain.ErrUnauthorized
	}
	if q.RuleVersion == "" || len(q.RuleVersion) != 64 || q.From.IsZero() || !q.To.After(q.From) || q.From.Year() < 1970 || q.To.Year() > 9999 {
		return result, domain.ErrInvalid
	}
	rules, err := s.repository.Rules(ctx, user)
	if err != nil {
		return result, err
	}
	var rule *entity.Rule
	for i := range rules {
		if rules[i].AssetID == q.AssetID {
			rule = &rules[i]
			break
		}
	}
	if rule == nil {
		return result, domain.ErrNotFound
	}
	if rule.Version != q.RuleVersion {
		return result, domain.ErrConflict
	}
	maxRange := 3660 * 24 * time.Hour
	if rule.Kind == "ohlc" {
		maxRange = 31 * 24 * time.Hour
	}
	if q.To.Sub(q.From) > maxRange || q.To.After(time.Now().UTC()) {
		return result, domain.ErrInvalid
	}
	result.RuleVersion = rule.Version
	query := market.Query{AssetID: q.AssetID, Source: rule.Source, From: result.From, To: result.To, Interval: time.Duration(rule.IntervalSeconds) * time.Second, Limit: 2000}
	events := []entity.Event{}
	for {
		points := []entity.Point{}
		next := ""
		if rule.Kind == "daily" {
			page, e := s.market.Snapshots(ctx, query)
			if e != nil {
				return result, e
			}
			next = page.NextCursor
			for _, p := range page.Points {
				points = append(points, entity.Point{Time: p.Time, ObservedAt: p.Time, Price: p.PriceUSD})
			}
		} else {
			page, e := s.market.Trend(ctx, query)
			if e != nil {
				return result, e
			}
			next = page.NextCursor
			for _, p := range page.Candles {
				observed := p.Time.Add(query.Interval)
				if p.SourceTime != nil {
					observed = *p.SourceTime
				}
				points = append(points, entity.Point{Time: p.Time, ObservedAt: observed, Price: p.Close})
			}
		}
		for _, p := range points {
			result.Scanned++
			if result.Scanned > 50000 {
				return result, domain.ErrInvalid
			}
			if p.ObservedAt.After(time.Now().UTC()) {
				return result, domain.ErrInvalid
			}
			d, status, e := Evaluate(p.Price, rule.AnchorPrice, rule.WarningBPS, rule.CriticalBPS)
			if e != nil {
				return result, e
			}
			if status == "stable" {
				continue
			}
			direction := "above"
			price, _ := positive(p.Price)
			anchor, _ := positive(rule.AnchorPrice)
			if price.Cmp(anchor) < 0 {
				direction = "below"
			}
			event := entity.Event{UserID: user, AssetID: q.AssetID, Kind: rule.Kind, Source: rule.Source, IntervalSeconds: rule.IntervalSeconds, Time: p.Time, ObservedAt: p.ObservedAt, Price: p.Price, AnchorPrice: rule.AnchorPrice, DeviationBPS: d, Severity: status, Direction: direction, WarningBPS: rule.WarningBPS, CriticalBPS: rule.CriticalBPS, RuleVersion: rule.Version, DetectedAt: time.Now().UTC()}
			event.ID = identity.Digest(user + "|" + q.AssetID + "|" + rule.Version + "|" + p.Time.UTC().Format(time.RFC3339Nano))
			events = append(events, event)
		}
		if next == "" {
			break
		}
		if next == query.Cursor {
			return result, domain.ErrUnavailable
		}
		query.Cursor = next
	}
	result.Matched = len(events)
	result.Inserted, err = s.repository.Record(ctx, *rule, events)
	return result, err
}

func (s *service) Events(ctx context.Context, q entity.EventQuery) (entity.EventPage, error) {
	if q.UserID == "" {
		return entity.EventPage{}, domain.ErrUnauthorized
	}
	if len(q.AssetID) > 96 || len(q.Cursor) > 1024 || q.Limit < 1 || q.Limit > 100 {
		return entity.EventPage{}, domain.ErrInvalid
	}
	return s.repository.Events(ctx, q)
}
