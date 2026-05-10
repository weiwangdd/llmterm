# llmterm

> **免责声明.** llmterm 是一个第三方非官方工具。它通过子进程调用**你**自己安装并完成认证的 Claude Code / Codex CLI / Gemini CLI，使用的是**你**自己的订阅。它与 Anthropic、OpenAI、Google **没有任何附属关系，未获其背书**。llmterm 不读取、不存储、不传输、不代理你的任何凭据——只是把上游 CLI 当普通子进程拉起。共享账号、转发凭据、以 llmterm 为中转转售访问，均违反上游服务的使用条款，且**不被支持**。

> 中文 / [English](README.md)

在 iTerm2 / Ghostty / Terminal.app 等任意终端里，键入 `llm <自然语言>` 或 `llm! <自然语言>` 就能让 agent 帮你做事——查文件、改配置、运行命令、查询信息——全程不离开当前终端。底层是你已登录的 Claude Code（或 Codex / Gemini）订阅，llmterm 只是一层 zsh widget + Go 二进制的薄壳。

## 安装（macOS, zsh）

前置要求：Go ≥ 1.22；以及至少一个上游 CLI 已装且已登录：
- [Claude Code](https://docs.anthropic.com/claude/docs/claude-code)（默认后端）
- [Codex CLI](https://github.com/openai/codex)（可选）
- [Gemini CLI](https://github.com/google-gemini/gemini-cli)（可选）

**一键安装**：

```sh
curl -fsSL https://raw.githubusercontent.com/weiwangdd/llmterm/main/install.sh | bash
```

脚本会：① 用 `go install` 装好二进制；② 调 `llmterm onboard` 让你选默认后端、可选地把 `eval` 行写进 `~/.zshrc`。完事开新 shell 即可。

**从源码装**：

```sh
git clone https://github.com/weiwangdd/llmterm
cd llmterm && make install
echo 'eval "$($HOME/.local/bin/llmterm init zsh)"' >> ~/.zshrc
exec zsh
llmterm doctor
```

`llmterm doctor` 会列出三个后端的安装/认证状态，并用 `*` 标出当前激活的那个。

## 用法

### 提示符里的两种前缀

```sh
llm  这个目录里有什么文件
llm  总结一下最近 5 个 commit
llm! 把当前目录所有 .jpeg 改成 .jpg
llm! 创建一个 python venv 并安装 ruff
llm! 当前系统内存占用率
```

| 前缀 | 允许的工具 | 适用场景 |
|---|---|---|
| `llm <prompt>` | 只读：`Read` `Glob` `Grep` `WebFetch` `WebSearch` | 查询、读文件、搜索、查网页——不会改你的系统 |
| `llm! <prompt>` | 在只读基础上加 `Bash` `Edit` `Write` | 让 agent 真正动手——改文件、跑命令、写新文件 |

如果你用 `llm` 但任务实际需要写/执行，agent 会给出一行简洁提示，例如：

```
(needs Bash; rerun: llm! 当前系统内存占用率)
```

`Ctrl-C` 中途取消，干净退回 prompt。

### 切换后端

```sh
llm use            # 切回默认（claude）
llm use codex      # 切到 OpenAI Codex
llm use gemini     # 切到 Google Gemini
```

切换时会显示一张确认卡片，包含上游 CLI 的实际版本：

```
╭──────────────────────────────────────────────────────╮
│ llmterm → claude                         Anthropic │
│ via claude 2.1.138 (Claude Code)                    │
│ third-party wrapper · not affiliated                │
╰──────────────────────────────────────────────────────╯
```

颜色按 vendor 区分：Anthropic 橙、OpenAI 青绿、Google 蓝。**没有**复刻任何上游 CLI 自己的欢迎画面或 logo——只用名字 + 颜色 + 框线做识别。

切换后续会写入 `~/.config/llmterm/config.toml`，跨 session 持久。

### 同目录续会话

同一个 cwd 下两小时内的多次 `llm` 调用会自动 `--resume` 上一轮的 session，agent 记得上下文：

```sh
$ cd ~/proj/foo
$ llm 这个仓库的 license 是什么
MIT.
─ 1 turn(s) · 2.4s · $0.0928
$ llm 总结主要文件
（agent 知道你还在问 ~/proj/foo）
```

会话索引存在 `~/.local/state/llmterm/sessions.json`，按 `<backend>:<cwd>` 索引——切后端不会窜会话。

## 工作原理

```
zsh prompt   ──ZLE widget──▶  llmterm run --[unsafe]   ──子进程──▶  claude -p / codex exec / gemini --prompt
                                       │
                                       ▼
                              解析 NDJSON / JSONL 事件
                                       │
                                       ▼
                              渲染：流式 assistant 文本
                                     ▸ 工具调用 单行
                                     ✓ 工具结果 折叠
                                     ─ 终态摘要（耗时 / cost）
```

不劫持 PTY、不重写 OAuth、不维护 token 缓存——所有 agent 能力都来自上游 CLI 自己的 headless 模式：
- `claude -p --output-format=stream-json --include-partial-messages`
- `codex exec --json`
- `gemini --prompt`

llmterm 只做三件事：① zsh widget 拦前缀；② 调子进程；③ 把上游事件流翻成统一的 TTY 渲染。

## 子命令

```
llmterm run [--unsafe] -- <prompt...>     执行一次提示，发给当前后端
llmterm use [claude|codex|gemini]         切换后端（无参数 = claude）
llmterm doctor                            体检：列出每个后端的安装/认证情况
llmterm init zsh                          打印 zsh 集成脚本（用 eval 加载）
llmterm version                           打印版本号
```

## 当前 MVP 范围

支持：macOS、zsh、Claude Code / Codex / Gemini 后端。

暂不支持（计划在 v2）：
- bash、fish、PowerShell
- Linux、Windows
- 多行 / heredoc 内联输入
- 持久化 transcript 浏览界面
- 设置 UI（目前只能编辑 TOML）
- 任何遥测

## 合规边界

| 行为 | 是否合规 |
|---|---|
| 个人在自己机器上用自己账号跑 | ✅ 等同 zsh alias |
| 团队内每人用各自的订阅 | ✅ |
| 多人共用一个账号 / 把 token 塞进 llmterm 配置分发 | ❌ 违反上游服务条款 |
| 做成 SaaS 用你的 Claude/OpenAI 订阅给最终用户跑 agent | ❌ 应改走商业 API |
| 二进制名/包名/域名使用 `Claude` `Anthropic` `Codex` `OpenAI` `Gemini` `Google` 等商标 | ❌ 商标问题 |

llmterm 自身设计上"纯子进程外壳 + 无凭据接触"就是为了让你保持在第一行的合规区。

## License

MIT.
