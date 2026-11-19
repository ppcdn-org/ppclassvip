# PPClassVip P1 验收状态

| 项 | 内容 |
| --- | --- |
| 日期 | 2026-11-02 |
| 后端 | `bin/ppclassvip.exe -config conf/config.local.yaml`，`:18080` |
| 前端 | `ppclass-web`，`http://127.0.0.1:5173` |
| 数据库 | 本机 MySQL 8，库 `ppclass` |
| PPCDN | 已启用，`https://api.pp-cdn.org`，appId/deviceId 见本机 `bin/.env`（未提交） |
| 浏览器 | Chrome（Playwright headless），摄像头/麦克风使用 Chrome 假设备，屏幕使用 canvas 模拟 `getDisplayMedia` |

## 已自动验证

### 构建与单元测试

- `go build ./...`
- `go test ./...`
- `go vet ./...`
- `ppclass-web: npm run build`
- 覆盖：PPCDN 客户端签名/凭据、附件魔数与文件名、限流、日志脱敏、WS 事件信封兼容、附件存储。

### 真实 PPCDN 媒体链路

- 教师/学生摄像头+麦克风双向发布/订阅：双方远端画面 `1280×720`，音视频轨 `live`。
- 屏幕共享：教师屏幕推流成功，学生主画面切换到教师屏幕。
- 停止屏幕共享：学生主画面恢复教师摄像头。
- 录屏：教师发起 → 学生同意 → 教师端本地录制 → 保存为本地 WebM 文件（服务端不存储）。
- 录屏产物：WebM，VP9 `1280×720` 视频 + Opus 音频；`ffmpeg volumedetect` 测得 `mean_volume≈-18 dB`，音频非静音。
- 课堂结束：双方正常退出。

### Windows Agent 独立采集与远程协助

- Agent 0.2.0 配对后独立启动无头 Chrome 采集屏幕，通过 WHIP 发布 `student-screen`；学生浏览器不再调用 `getDisplayMedia`。
- 教师在独立 WHEP 客户端订阅 `student-screen`：H264 `1728×1080`，`framesDecoded=86`，画面非黑。
- 控制通道发送 `mouse.move`，本机光标从 `(426,266)` 移到 `(1279,799)`。
- Agent 安装包可从 `/downloads/ppclass-agent-setup.exe` 安装，写入 HKCU 开机启动；卸载后目录和注册表值均清除。
- 配对后显示本地 Windows 同意弹窗；拒绝时调用 `/api/v1/agent/stop`，会话状态变为 `stopped`，原因 `local_declined`。
- 后端重启造成 502 中断后 Agent 自动重新配对并恢复控制 WS；随后用 `tc netem delay 150ms 30ms loss 20%` 叠加后端重启，弱网结束后 Agent 再次恢复采集与控制。
- 会话停止后 Agent 关闭采集浏览器并清理临时目录。

### 断网与重连

- 学生浏览器离线 10 秒后恢复：
  - WS 自动重连成功；
  - 离线期间教师发送的消息通过 `afterId` 补拉成功；
  - 双方媒体重新发布/订阅，远端音视频恢复。

### 业务与安全

- 邀请 token 复用被拒绝（409）。
- 1v1 课堂第二名学生加入被拒绝（`room_full`）。
- 图片上传、下载内容一致；`.exe` 上传被拒绝（`attachment_type_forbidden`）。
- 课堂结束后媒体凭据续签被拒绝（`room_ended`）。
- 日志中 `token` 查询参数被脱敏为 `redacted`。

### Reaper（通过修改测试数据时间触发）

- 录屏待确认超时：`pending_consent → declined`。
- 附件保留期到期：状态 `purged`，文件删除。
- 空闲课堂回收：状态 `ended`，`end_reason=timeout`。

## 需要人工验证

以下项目依赖真实设备、真实网络或人工判断，自动化无法覆盖：

| 编号 | 项目 | 手工步骤 | 预期 |
| --- | --- | --- | --- |
| M1 | 真实摄像头/麦克风 | 两台设备分别用教师/学生账号进入课堂，开启音视频 | 双方画面清晰、声音正常、无明显回声 |
| M2 | 真实屏幕/窗口共享 | 教师点击“共享屏幕”，选择真实屏幕/窗口 | 学生看到所选画面；停止后恢复摄像头 |
| M3 | 录屏双方声音 | 教师录屏，教师和学生分别说话，录制结束后播放本地文件 | 能清楚听到双方声音，无重复/严重回声 |
| M4 | 真实断网切换 | 学生切换 Wi-Fi/蜂窝网络或断网 10 秒 | 自动重连；聊天补拉；音视频恢复 |
| M5 | 跨浏览器 | Chrome/Edge 教师，Safari/Firefox 学生 | 基础观看/聊天可用；Safari 无 WebM 录屏时按钮禁用 |
| M6 | 弱网 | 限制上行/下行、丢包 5%–10% | 音频优先，画面降级但可继续上课 |
| M7 | 50 并发课堂 | 测试环境并发创建/加入 50 个课堂 | 加入失败率 < 1%，Anchor 容量告警可触发 |
| M8 | 长时间课堂 | 连续上课 1–2 小时并持续录屏 | 媒体不中断，本地录制持续进行并可正常保存 |
| M9 | 存储/磁盘边界 | 构造大文件、超配额、磁盘写满 | 拒绝新上传，不影响已建立音视频 |
| M10 | 真实设备绑定流程 | 新机器首次发布 | 自动注册 ppobs 设备或按文档配置 `PPCLASS_PPCDN_DEVICE_ID` |
| M11 | 计费/欠费 | ppcdn 账号欠费或配额耗尽 | 新课堂/发布被拒绝，已建立课堂有宽限提示 |
| M12 | 安全人工复核 | 检查越权、房间隔离、附件下载 | 不能跨房间访问；服务端无录像下载入口 |

## 已知限制（非缺陷但上线前需确认）

- 前端暂未周期上报 `media.stats`；后端已支持存储。
- 暂无指标面板/告警，只有 `/healthz`、`/readyz` 和结构化日志。
- Hub、限流器是进程内状态，当前只支持单实例部署。
- 屏幕共享拉流在 Edge pipeline 未注册时会产生短暂 404，客户端会自动重试；控制台可能有瞬时错误日志。
- 录屏在教师浏览器完成并保存到本机；页面刷新或浏览器崩溃会丢失尚未保存的录制。
- 教师端优先 Chrome/Edge；Safari 可能不支持 WebM `MediaRecorder`。
- 生产必须使用 HTTPS：摄像头、麦克风、屏幕共享要求安全上下文。

## 快速回归命令

```powershell
cd D:\gitlab\ppcdn\ppClassVip
go build ./...
go test ./...
go vet ./...

cd ppclass-web
npm run build
npm run dev
```

后端运行目录：

```powershell
cd D:\gitlab\ppcdn\ppClassVip\bin
.\ppclassvip.exe -config conf/config.local.yaml
```
