# 连接 Aria Code

`aria-plugins` 是 Aria Code 的插件目录。`commerce` 是第一个插件，Go 服务和网页是这个插件使用的工具与示例。之后可以在同一目录增加其他服务，每个插件保留独立名称、版本、技能和 MCP 配置。

## 1. 安装技能

在支持技能目录安装的 Aria Code 交互会话中输入：

```text
/skills add artheras/aria-plugins
/skills doctor
```

应该发现 `commerce:commerce`，完整性为 `verified`。需要固定版本时：

```text
/skills add artheras/aria-plugins --ref <完整提交SHA或发布标签>
```

当前 Aria 安装器拒绝覆盖已安装目录，不会自动升级。更新前保留需要的本地修改，再显式移除或更新该目录；本仓库没有自动删除用户目录的脚本。如果没有 `/skills add`，先确认客户端版本包含 [目录安装实现](https://github.com/artheras/aria-code/blob/main/src/aria_code/apps/cli/commands/model_cmds.py)。

目录通常位于 Aria 状态目录下的 `skill-catalogs/artheras/aria-plugins`。状态目录优先使用 `ARIA_HOME`，其次沿用已存在的 `~/.arthera`，新用户默认 `~/.aria-code`。`ARIA_SKILL_CATALOG_HOME` 可以单独指定技能目录。

`.claude-plugin/marketplace.json` 的名字沿用当前 Aria 加载器的兼容格式。Aria 读取根目录 `skills/*/SKILL.md`；插件原稿在 `plugins/commerce/skills/commerce`。Go 工具生成相同内容的根目录副本以及完整性校验文件。

## 2. 编译并运行 Commerce

安装 Go 1.26+，在独立的仓库工作目录中执行：

```sh
git clone https://github.com/artheras/aria-plugins.git
cd aria-plugins
make build
make catalog-check
export ARIA_COMMERCE_URL=http://127.0.0.1:4180
export ARIA_MERCHANT_TOKEN="$(openssl rand -hex 32)"
./bin/aria-commerce
```

这一步默认使用演示模式。保存该会话生成的商户 Token 到自己的本地配置，让服务、Aria MCP 进程和网页商户会话使用同一值。不要重新生成另一份 Token 后仍连接原服务，也不要把 Token 提交到仓库。

## 3. 注册 MCP 工具

将 [mcp.json](../plugins/commerce/mcp.json) 的 server 条目合并到实际 Aria 状态目录的 `mcp_servers.json`，保留其他服务器。

- 把 `command` 换为本机 `bin/aria-commerce-mcp` 的真实绝对路径。
- 启动 Aria Code 的 shell 中导出相同的 `ARIA_COMMERCE_URL` 和 `ARIA_MERCHANT_TOKEN`。
- 当前 Aria 只展开 `args` 和 `env` 内的 `${变量名}`，不会展开 `command`，因此 `command` 不能保留环境变量占位符。
- 在配置保存后重新启动 Aria 会话，使它重新发现 MCP 工具。

工具包括 `commerce_catalog`、`commerce_draft_order`、`commerce_get_order`。模型看到的工具名可能带 `mcp__aria-commerce__` 前缀。

## 4. 调用插件并检查结果

在 Aria 中输入：

```text
$commerce:commerce 生成这个演示订单的草稿：2 tees and 1 mug
```

Aria 查询商品目录并生成草稿后，给出订单 ID。在 `http://127.0.0.1:4180` 点击 Merchant session，填写同一商户 Token，然后打开 `/?order=订单ID`。核对后，用户在网页完成确认、结账和模拟支付。代理不能通过 MCP 执行这些动作。

演示模式使用固定解析器，不调用 AI，不移动资金。真实 AI 和 PayPal Sandbox 按 [配置文档](setup.zh-CN.md) 设置；缺少配置时不会伪装成功。

## 5. 当前安装边界与后续扩展

现有 `/skills add` 安装、发现和验证技能。它不会构建 Go 二进制、启动服务、导入 MCP 配置或把 `commands/commerce.md` 注册成新的 slash 命令。

完整的插件安装体验需要在 `aria-code` 主程序实现目录索引、组件注册、依赖配置和版本更新。`aria-plugins` 已提供这些组件的发布结构；当前按上面的技能 + MCP 两步接入。

Claude 格式的 manifest 与 `.mcp.json` 也已保留。使用该配置时先把构建出的 `bin/` 加入 PATH，并导出服务 URL 与 Token。Claude Code 的完整安装流程仍需单独验证，不能将它的 `/plugin install` 命令直接当作 Aria 已实现的功能。

参考：[Anthropic 官方插件目录](https://github.com/anthropics/claude-plugins-official)、[Aria 技能加载器](https://github.com/artheras/aria-code/blob/main/src/aria_code/packages/aria_skills/loader.py)、[Aria MCP 客户端](https://github.com/artheras/aria-code/blob/main/src/aria_code/mcp_client.py)。
