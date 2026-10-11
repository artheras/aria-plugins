# 中文快速配置

这次业务代码使用 Go，网页使用原生 HTML/CSS/JavaScript。仓库提供 Commerce 应用以及可复用的 agents、approvals、payments、store 模块。

## 现在就能体验

安装 Go 1.26+，在仓库目录执行：

```sh
make build
./bin/aria-commerce
```

打开 `http://127.0.0.1:4180`，依次生成订单、确认内容、创建结账、模拟支付、下载收据。默认样例是 2 件 T 恤和 1 个杯子，总价 60 美元。

**演示模式没有调用 AI，也没有发起 PayPal 交易。** 页面与收据均有明确标记。订单记录保存在 data 目录，刷新和服务重启后仍可查询。

## 接入真实 AI 与 PayPal Sandbox

复制 .env.example 到 .env，在本机填写：

| 变量 | 来源或用途 |
| --- | --- |
| ARIA_MODE | 改为 sandbox |
| PUBLIC_URL | 浏览器使用的准确网址；线上使用 HTTPS |
| PAYPAL_CLIENT_ID / PAYPAL_CLIENT_SECRET | PayPal 开发者后台的 Sandbox 应用 |
| AI_GATEWAY_API_KEY | Vercel AI Gateway 的 API 密钥 |
| AI_GATEWAY_MODEL | 从实时模型列表选择支持 JSON 输出的模型 ID |
| ARIA_SESSION_SECRET | 自己生成的随机值，至少 32 字符 |
| ARIA_MERCHANT_TOKEN | 另一份独立随机值，至少 32 字符 |
| PAYPAL_WEBHOOK_ID | 可选；接收真实支付回调时必须配置 |

Go 服务不会自动读取 .env，需要先在 shell 导出，再启动：

```sh
set -a
. ./.env
set +a
./bin/aria-commerce
```

进入网页后用 ARIA_MERCHANT_TOKEN 连接商户会话。确认订单后打开 PayPal Sandbox，使用测试买家账户批准支付，返回网页点击捕获并核对。

## Vercel 与 Aria Code

Vercel 新建项目导入 aria-plugins，根目录选仓库根，框架选 Go。必须配置持久化 Redis 的 REST URL、读写 Token、稳定的会话密钥以及准确的 PUBLIC_URL。服务器不会在 Vercel 上用临时文件保存支付记录。

Aria MCP 配置样板位于 examples/mcp_servers.json。编译后填写 Go MCP 二进制的绝对路径，并让它与网页服务使用相同的商户 Token。MCP 可读取商品、生成草稿、查询订单；确认与支付由用户在网页中操作。

完整步骤见 [英文配置文档](setup.md)。当前已通过本地演示与模拟接口测试；真实 AI、PayPal Sandbox、Aria 客户端集成和线上部署需要配置后再验证。
