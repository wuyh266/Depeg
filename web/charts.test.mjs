import test from 'node:test';
import assert from 'node:assert/strict';
import {sampleExtrema,splitSegments,csvCell,comparisonSeries} from './static/charts.js';

test('chart reduction retains a brief crash and spike',()=>{
  const points=Array.from({length:10000},(_,i)=>({t:i*60000,y:1}));
  points[4001].y=.01;points[4002].y=1.9;
  const sample=sampleExtrema(points,500);
  assert.ok(sample.length<600);
  assert.ok(sample.includes(points[4001]));assert.ok(sample.includes(points[4002]));
  assert.equal(sample[0],points[0]);assert.equal(sample.at(-1),points.at(-1));
  for(let i=1;i<sample.length;i++)assert.ok(sample[i].t>sample[i-1].t);
});
test('missing timestamps and null values do not produce connecting lines',()=>{
  const segments=splitSegments([{t:0,y:1},{t:60000,y:1},{t:300000,y:1},{t:360000,y:NaN},{t:420000,y:1}],60000);
  assert.deepEqual(segments.map(s=>s.length),[2,1,1]);
});
test('CSV preserves decimals and empty values while escaping formulas and quotes',()=>{
  assert.equal(csvCell('0.000000000000000001'),'"0.000000000000000001"');
  assert.equal(csvCell(null),'""');assert.equal(csvCell('-50.000000'),'"-50.000000"');
  assert.equal(csvCell('=HYPERLINK("bad")'),'"\'=HYPERLINK(""bad"")"');
  assert.equal(csvCell('source,"quoted"'),'"source,""quoted"""');
});

test('comparison normalizes each asset from its own first observed price',()=>{
  const series=comparisonSeries([
    {id:'a',records:[{time:'2023-03-10T00:00:00Z',price:'2'},{time:'2023-03-11T00:00:00Z',price:'2.2'}]},
    {id:'b',records:[{time:'2023-03-12T00:00:00Z',price:'10'},{time:'2023-03-13T00:00:00Z',price:'9.5'}]}
  ],'change');
  assert.equal(series[0].points[0].y,0);assert.equal(series[1].points[0].y,0);
  assert.ok(Math.abs(series[0].points[1].y-10)<1e-10);
  assert.ok(Math.abs(series[1].points[1].y+5)<1e-10);
  assert.notEqual(series[0].points[0].t,series[1].points[0].t);
});

test('comparison preserves decimal strings, null gaps and independent source data',()=>{
  const records=[
    {time:'2023-03-12T00:00:00Z',price:'1.000000000000000001'},
    {time:'2023-03-10T00:00:00Z',price:null},
    {time:'2023-03-11T00:00:00Z',price:'0.900000000000000001'},
    {time:'2023-03-13T00:00:00Z',price:null},
    {time:'2023-03-15T00:00:00Z',price:'1.01'}
  ];
  const before=JSON.stringify(records),[series]=comparisonSeries([{id:'a',records}]);
  assert.equal(JSON.stringify(records),before);
  assert.ok(Number.isNaN(series.points[0].y));
  assert.equal(series.points[2].price,'1.000000000000000001');
  assert.deepEqual(splitSegments(series.points,86400000).map(s=>s.length),[2,1]);
});

test('comparison handles empty and missing-price series without inventing zero prices',()=>{
  const empty=comparisonSeries([{records:[]}],'change');assert.deepEqual(empty[0].points,[]);
  const [missing]=comparisonSeries([{records:[{time:'2023-03-10T00:00:00Z',price:null},{time:'2023-03-11T00:00:00Z'}]}],'change');
  assert.ok(missing.points.every(p=>Number.isNaN(p.y)));
});
