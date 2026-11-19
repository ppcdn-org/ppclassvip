# 远程桌面（Windows Agent）

## 范围

- 教师发起远程桌面请求，学生同意/拒绝。
- 支持 `view_only`（只看）和 `control`（控制鼠标键盘）。
- 学生必须安装并启动 Windows Agent；屏幕由 Agent 独立采集并发布，学生浏览器只负责同意和显示配对码。
- 单次会话最长 **30 分钟**；请求确认超时 **60 秒**。
- **不录制远程控制过程**：远程桌面活跃时禁止开始课堂录屏，录屏活跃时禁止发起远程桌面。
- 首期只支持 Windows Agent。

## 链路

```text
教师浏览器
  ├─ 课堂 WS：请求 / 同意 / 状态
  ├─ WHEP：订阅 student-screen
  └─ 控制 WS：发送 mouse/key

PPClassVip
  ├─ remote_desktop_sessions
  ├─ 同意与 30 分钟超时
  ├─ 控制 WS 中继（controller → agent）
  ├─ Agent 配对码
  └─ 审计

学生浏览器
  ├─ 同意弹窗
  └─ 显示 Agent 配对码

PPClass 远程协助 Agent（Windows）
  ├─ 托盘 UI / 本地同意弹窗
  ├─ 启动独立无头浏览器采集屏幕
  ├─ WHIP 发布 student-screen
  ├─ 连接控制 WS
  └─ SendInput 注入鼠标/键盘
```

## 流程

1. 教师点击“远程桌面”，选择只看或控制。
2. 服务端创建 `pending_consent` 会话，并给学生课堂 WS 发送 `remote_desktop.request`。
3. 学生浏览器弹窗，选择：
   - 允许只看
   - 允许控制
   - 拒绝
4. 学生同意后：
   - 服务端返回 `agentCode`（只看和控制模式都会返回）。
   - 学生浏览器不再调用 `getDisplayMedia`。
   - 学生安装并启动 Agent，在托盘菜单输入配对码。
5. Agent 调 `POST /api/v1/agent/pair` 换取短时 agent token、`student-screen` 发布凭据，连接控制 WS 并开始发布屏幕。
6. 教师收到 `remote_desktop.consented`，连接控制 WS 并显示控制层。
7. 学生可随时停止；教师可停止；30 分钟到期、房间结束、Agent 断线都会自动结束。

## API

```text
POST /api/v1/classrooms/:id/remote-desktop/request
POST /api/v1/classrooms/:id/remote-desktop/:sid/consent
POST /api/v1/classrooms/:id/remote-desktop/:sid/decline
POST /api/v1/classrooms/:id/remote-desktop/:sid/stop
GET  /api/v1/classrooms/:id/remote-desktop/:sid
GET  /api/v1/classrooms/:id/remote-desktop/:sid/control   # 控制 WS
POST /api/v1/agent/pair                                  # Agent 用配对码换 token
```

控制 WS 使用子协议：

```text
Sec-WebSocket-Protocol: ppclass.control.<token>
```

控制事件：

```text
{"type":"mouse.move","x":0.5,"y":0.5,"ts":...}
{"type":"mouse.button","button":"left|right|middle","action":"down|up"}
{"type":"mouse.wheel","dx":0,"dy":120}
{"type":"key","code":"KeyA","action":"down|up","modifiers":["ctrl"]}
```

坐标归一化为 `0..1`，Agent 映射到 Windows 虚拟桌面。

## Agent

- 安装包：`https://ppclassvip.pp-cdn.org/downloads/ppclass-agent-setup.exe`
- 安装到 `%LOCALAPPDATA%\PPClass\Agent`，写入 HKCU 开机启动，并把 Agent 放入系统托盘。
- Agent 启动时检查 `/downloads/ppclass-agent.latest.json`，校验 SHA-256 后自动更新并重启。
- 托盘菜单支持输入配对码、检查更新、断开当前会话、退出。
- 配对成功后显示本地 Windows 同意弹窗；拒绝则不上传屏幕、不连接控制 WS。
- 仅支持当前 Windows 用户权限注入：
  - 可控制普通桌面应用。
  - 无法控制 UAC 安全桌面、登录界面、以管理员权限运行且未提权的窗口。
- 暂不支持剪贴板、文件传输、系统音频、Ctrl+Alt+Del。
- 控制 WS 断开后 Agent 会用同一配对码自动重连/重新配对；配对码过期或会话结束后才需要重新输入。
- 屏幕采集通过 Agent 启动的独立无头 Chrome/Edge 完成，不依赖学生课堂页面。

## 安全

- 配对码 8 位、5 分钟有效、只存哈希、按 IP 限流。
- 控制 token 绑定 `sessionId + roomId + teacher/student participant + role`，短 TTL。
- 学生每次会话必须重新同意，可随时停止。
- 审计记录请求、同意、拒绝、开始、停止和原因；不记录键盘内容。
- 远程桌面期间禁止录屏。

## 已知限制

- 屏幕采集依赖本机已安装 Chrome 或 Edge（Windows 10/11 默认有 Edge）。
- 采集使用 `--auto-select-desktop-capture-source=Entire screen`，非英文系统如自动选屏失败需要手工验收。
- 本地同意弹窗是浏览器同意之外的第二道确认。
- 只支持 Windows 输入注入，macOS/Linux 后续。
- UAC/管理员权限窗口无法控制。
- 教师端控制层需要点击画面捕获键盘，`Esc` 释放键盘。
- 断线重连已用后端重启和 20% 丢包/150ms 延迟的 netem 弱网场景验证。
