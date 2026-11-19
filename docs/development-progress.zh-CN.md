# PPClassVip 开发进度总结

> 更新时间：2026-11-02  
> 分支：`master`  
> 线上地址：`https://ppclassvip.pp-cdn.org`（管理后台 `/admin/`）

## 1. 当前状态

已具备可完整上一节 1v1 在线课的能力，并额外完成了 Windows 远程协助 Agent：

- 教师登录、创建课堂、生成邀请链接、学生游客加入。
- 双向音视频、屏幕共享、文字聊天、图片/文件、断线重连与消息补拉。
- 教师端录屏：屏幕 + 教师麦克风 + 学生远端声音，WebM 分片上传与下载。
- 独立管理后台：用户管理、课堂管理、附件/录屏管理、审计、动态平台设置。
- 远程桌面：学生同意、Agent 配对、独立屏幕采集、只看/控制、30 分钟超时、审计。
- Windows Agent 0.2.0：安装包、托盘 UI、本地同意、开机启动、自动更新、断线重连。

线上依赖：

- 业务后端：Go + MySQL；媒体面：PPCDN Origin/Edge（H264 WHIP/WHEP）。
- 数据库经 VPS 上的 SSH 隧道访问，本机不运行 MySQL。
- 前端为静态构建，`ppclass-web` 与 `ppclass-admin` 由 nginx 托管。

## 2. 已完成能力

### 2.1 课堂核心（P1）

- 课堂状态机：`created → waiting → active → ended/failed/expired`。
- 邀请链接、游客 join、room token、教师/学生权限隔离。
- 课堂 WS：在线状态、聊天、屏幕状态、录屏状态、远程桌面事件。
- 聊天消息持久化、分页、`afterId` 补拉、图片/文件上传下载。
- 屏幕共享独立媒体腿，同一时间只允许一方共享。
- 录屏同意、分片上传、finalize、下载、保留期清理。
- 限流、审计、空闲课堂 reaper、附件/录屏 reaper。
- 健康检查：`/healthz`、`/readyz`。

### 2.2 管理后台

- 独立 `ppclass-admin`，仅 `admin` 角色可访问。
- 用户禁用/启用、课堂结束、附件/录屏删除。
- 平台设置动态生效（邀请 TTL、配额、限流、录屏保留期等）。
- 审计查询、教师/管理员修改密码。

### 2.3 远程桌面 + Windows Agent

- 教师发起 `view_only` / `control` 请求，学生浏览器同意/拒绝。
- Agent 配对码：8 位、5 分钟、哈希存储、限流。
- Agent 调 `/api/v1/agent/pair` 换取短时 token 和 `student-screen` 发布凭据。
- Agent 独立启动无头 Chrome/Edge 采集整屏，通过 WHIP 发布 `student-screen`，不依赖学生课堂页面。
- 教师 WHEP 订阅学生屏幕；控制通道将鼠标/键盘事件中继给 Agent，由 `SendInput` 注入。
- 本地 Windows 同意框；拒绝时调用 `/api/v1/agent/stop` 结束会话。
- Agent 托盘菜单：输入配对码、检查更新、断开当前会话、退出。
- 安装包安装到 `%LOCALAPPDATA%\PPClass\Agent`，写入 HKCU 开机启动；支持卸载。
- 自动更新：读取 `/downloads/ppclass-agent.latest.json`，校验 SHA-256 后替换并重启。
- 控制 WS 断开后使用同一配对码自动重新配对/重连，指数退避。

### 2.4 已实测结果

- 真实 PPCDN 双向音视频、屏幕共享、录屏（VP9 + Opus，`mean_volume≈-18dB`）。
- 学生浏览器离线 10 秒后 WS 重连、消息补拉、媒体恢复。
- Agent 独立采集：教师订阅 H264 `1728×1080`，`framesDecoded=86`，画面非黑。
- 控制注入：光标从 `(426,266)` 移动到 `(1279,799)`。
- 安装/卸载：目录、开机启动注册表项、托盘 Agent 启动均验证。
- 弱网：`netem delay 150ms 30ms loss 20%` + 后端重启，弱网结束后 Agent 自动恢复采集与控制。
- `go build ./...`、`go test ./...`、`go vet ./...`、`ppclass-web npm run build` 通过。

## 3. 尚未完成

### P2 教学闭环

- 课程、排课、预约、教师课程表。
- 正式学生账号（当前仍为游客邀请加入）。
- 课堂历史、课程记录。
- 回放页、回放分享链接、下载权限与保留期管理界面。
- 文件预览、课后资料、聊天导出。
- 学生端未读/持久化已读、消息删除、敏感词/举报。
- 管理后台监控指标与告警面板。

### Agent 后续

- macOS/Linux Agent。
- 原生屏幕采集（当前由 Agent 启动无头 Chrome/Edge 完成）。
- UAC/管理员权限窗口控制。
- 剪贴板、文件传输、系统音频、多显示器选择。
- Agent 安装包签名、卸载入口、自动更新灰度/回滚。
- 真实非英文 Windows 的自动选屏兼容性验收。

### 生产化

- MySQL 备份/恢复、日志轮转、监控告警。
- 50 并发课堂、长时间课堂、跨浏览器（Safari/Firefox）人工验收。
- 安全复核：越权、限流、配额、数据保留与删除。

### P3 增强

- P2P 直连、白板/标注、作业、课程支付、移动端、E2EE、AI 转写。

## 4. 关键文档

- 设计与验收：[p1-acceptance-status.zh-CN.md](p1-acceptance-status.zh-CN.md)
- 远程桌面与 Agent：[remote-desktop.zh-CN.md](remote-desktop.zh-CN.md)
- 总体方案：[roadmap/ppclassvip-1v1-video-teaching.zh-CN.md](roadmap/ppclassvip-1v1-video-teaching.zh-CN.md)
