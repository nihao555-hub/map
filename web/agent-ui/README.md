# 地图获客 · Agent UI（豆包式）

独立 React 工作区，使用 [Vercel AI Elements](https://elements.ai-sdk.dev/) 风格组件（Conversation / Message / ChainOfThought / Task / Suggestion / PromptInput）。

## 开发

```bash
cd web/agent-ui
npm install
npm run dev   # http://localhost:5173/agent/  (API 代理到 :18080)
```

## 构建（嵌入 Go）

```bash
npm run build   # 输出到 web/static/agent/
# 然后重新 go build，访问 /agent/
```
