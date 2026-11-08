# 稳定币观察台前端

原生 HTML/CSS/JavaScript，由 web/embed.go 嵌入 Go 可执行文件。无外部 CDN、字体、图表运行时依赖；开发和启动不需要 npm。界面为中文，提供桌面与手机布局。

- static/index.html：入口与可访问性基础。
- static/app.css：主题、布局与响应式样式。
- static/app.js：登录/注册、单次刷新轮换、页面路由、总览、详情、日期查询、规则、历史检测与 CSV。
- static/charts.js：SVG 图表、保留极值的显示采样、缺口断线、鼠标/方向键查看与 CSV 转义。
- charts.test.mjs：可选的 Node 内置测试；执行 `node --test web/charts.test.mjs`。

在项目根目录运行 `go run -buildvcs=false ./cmd/api`，访问 http://127.0.0.1:8080/。先执行迁移，具体见根 README。嵌入文件修改后需要重启 Go 服务。

所有数据请求都发往同源 /api/v1，使用现有 JWT 和 MySQL。令牌保存在当前标签页的 sessionStorage；存储不可用时仅保存在内存。不保存密码，不把令牌写入 URL。刷新失败或数据库错误会明确提示；没有样例行情回退。

图表拉取指定区间的全部分页，最多 50,000 条；只对 SVG 显示做保留局部极值的采样。统计和 CSV 使用完整读取结果。导出保留金额原始十进制字符串与 NULL 空值；页面图表和格式化统计使用浏览器数值，仅用于展示。后端阈值判断使用精确有理数比较。

前端没有直接调用 CoinGecko、DefiLlama 或交易所，不需要外部 API 密钥。当前数据为导入的历史档案；监测为手动区间检测。模型预测未接入，不显示模拟分数。
