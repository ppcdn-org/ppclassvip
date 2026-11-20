# PPClassVip 1v1 视频教学系统方案

| 属性 | 值 |
| --- | --- |
| 文档状态 | 草案（roadmap，待评审）。独立后端已落地 Go 框架 + 课堂房间/邀请/游客加入/文字消息/实时 WS/结束 |
| 目标 | 基于 PPCDN 媒体面，构建单机构 1v1 在线教学课堂：双向音视频、屏幕共享、文字聊天、图片/文件、教师端本地录屏 |
| 适用仓库 | `ppClassVip`（Go 后端，独立仓库）、`ppclass-web`（教学前端）、`pplayer`（浏览器推流/播放/录屏 SDK）；媒体面使用 PPCDN/ppmmx，不改造 ppobs |
| 关联文档 | PPCDN 仓库：`ppcdn-1v1-video-call.zh-CN.md`、`ppcdn-shared-desktop-support.zh-CN.md`、`ppcdn-architecture-design.zh-CN.md`、`streaming.md`、`user-record-api.md` |
| 更新时间 | 2026-11-19 |

> 录屏范围更新：录屏在教师浏览器本地完成后直接保存为 WebM 文件，服务端只维护学生同意流程，不存储录像、不提供回放。下文涉及服务端分片存储、finalize 和回放的描述属于早期设计，以本节为准。

---

## 0. 结论速览

- **媒体面复用 PPCDN，业务面新增“课堂”模块。** 教师端和学生端都用浏览器 WebRTC：摄像头/麦克风走 WHIP 推流，对端画面走 WHEP 拉流；屏幕共享作为独立媒体腿上一条单独的 WHIP/WHEP，不和摄像头抢同一个 PeerConnection。
- **MVP 先做 Anchor 中继，不做 P2P。** 一次课堂最多 3 路 publisher（教师摄像头、学生摄像头、共享屏幕）和 3 路 reader，PPCDN 现有 Origin 中继即可承载，ppmmx 无需改媒体代码。教学场景 300–400ms 端到端延迟可接受，先把功能、稳定性和录屏做完整，再考虑 P2P 降成本。
- **业务层独立成 `ppClassVip` 服务，不塞进 ppcenter。** `ppClassVip` 用自己的 MySQL 8 存教师/课堂/聊天/附件/录屏，自己签发教师 JWT；PPCDN 只作为媒体控制面，提供 Anchor 选点、WHIP/WHEP 凭据和媒体就绪通知。两者通过内部 HTTP API 和配置的 app 凭证对接。
- **文字聊天和图片/文件不走 WebRTC DataChannel，走“WebSocket + HTTP 上传 + 签名下载”。** 这样可持久化、可审计、可断点续传、离线可看，也不受课堂连接是否还活着影响。
- **录制采用教师端录屏，不依赖网络侧 split-rec。** 教师在浏览器里用 `getDisplayMedia` + `MediaRecorder` 录制屏幕；音频用 Web Audio 混合“教师麦克风 + 学生远端声音”，不带教师摄像头小窗。录制在教师浏览器本地完成后直接保存为 WebM 文件，服务端只协调学生同意，不存储录像、不提供回放。
- **录学生声音必须单独征得学生同意。** 录屏开始前学生收到明确提示并确认；拒绝则不能开始录制，课堂中也可要求停止。
- **单机构部署。** 平台只有一个机构、一个媒体 app；教师由管理员配置，学生以游客邀请链接加入。不做多租户、SaaS 白标和按机构隔离。
- **首版不包含：** 多人课堂、白板、作业、课程支付、原生 App、E2EE、学生端录屏、教师摄像头小窗。这些放在 P2 以后，避免首版范围失控。

---

## 1. 产品范围

### 1.1 目标

- **TEACH-G1**：教师登录后创建课堂，生成邀请链接；学生凭链接进入，无需安装客户端。
- **TEACH-G2**：双方摄像头 + 麦克风双向互通，支持静音、关摄像头、设备切换。
- **TEACH-G3**：教师可共享整个屏幕/窗口/标签页；同一时刻只允许一方共享，学生共享需教师授权。
- **TEACH-G4**：课堂内文字聊天，支持图片和文件；消息有序、可重试、可回看。
- **TEACH-G5**：教师可开启教师端录屏；录屏内容为“屏幕主画面 + 教师麦克风 + 学生远端声音”，不带教师摄像头小窗；录制结束保存为教师本地单文件 WebM。
- **TEACH-G6**：弱网、断线重连、浏览器权限拒绝、屏幕共享中断等场景有明确降级和提示。
- **TEACH-G7**：房间、消息、文件、录制都有权限校验、保留期和审计，不把教学数据混进客服消息表。

### 1.2 分期范围

| 阶段 | 范围 | 目的 |
| --- | --- | --- |
| **P0 技术验证** | 两个浏览器经同一 Anchor 互推互拉；屏幕共享独立腿；聊天 WS；文件上传下载；教师端 MediaRecorder 本地录屏 | 验证媒体链路、浏览器兼容性和教师端录屏 |
| **P1 MVP** | 教师登录、创建课堂、邀请链接、学生游客加入、双人音视频、屏幕共享、文字/图片/文件、在线状态、断线重连、教师端录屏、结束课堂 | 可完整上一节 1v1 课并在教师本地保存录屏 |
| **P2 教学闭环** | 课程/排课/预约、课堂历史、权限控制、审计、保留期、文件预览、课后资料 | 可给真实教师/学生使用 |
| **P3 增强** | P2P 直连、白板/标注、作业、课程支付、移动端、E2EE、AI 转写 | 降成本、扩场景 |
| **不做（首版）** | 多人班课、网络侧多路录制再合成、PSTN/SIP、远程控制学生桌面、OBS 推流端接入 | 明确边界 |

### 1.3 关键产品假设

1. **先做 Web，桌面浏览器优先。** 教师端建议 Chrome/Edge；学生端支持 Chrome/Edge/Safari 16+。移动浏览器首版只保证“能进课堂、能看能听”，屏幕共享和录屏回放以桌面为主。
2. **学生以游客身份加入。** 教师必须登录；学生凭一次性邀请链接进入，输入昵称即可，不要求注册 PPCDN 账号。邀请只用于创建本次 participant，重连使用 room token；课后是否允许游客回看由教师分享单独链接决定。
3. **单机构、单媒体 app。** 平台只有一个机构和一个课堂 app（配置 `classroom.appId`）；教师由管理员配置，不做多租户、SaaS 白标和机构间隔离。
4. **同区域优先。** 教师和学生尽量在同一大区；跨大区可以上课，但延迟会上升，首版不承诺跨区低延迟。

已确认决策（2026-10-30）：

- 单机构部署，不做多租户。
- 学生允许游客加入，凭一次性邀请链接 + 昵称进入。
- 录制采用教师端 `MediaRecorder` 录屏，不做网络侧多路录制再合成。
- 录屏画面不带教师摄像头小窗。
- 录屏必须包含学生声音；学生需在录制开始前确认。
- 录屏交付 WebM；不要求 MP4，也不做服务端转封装。

---

## 2. 角色与核心流程

### 2.1 角色

| 角色 | 身份 | 权限 |
| --- | --- | --- |
| 教师 Teacher | 已登录平台用户，由管理员赋予 `classroom_teacher` 角色 | 创建课堂、邀请学生、开始/停止录屏、共享屏幕、静音学生、移出学生、结束课堂 |
| 学生 Student | 受邀游客，不要求注册 | 加入课堂、音视频、文字/图片/文件、经授权后共享屏幕、录制同意/拒绝 |
| 机构管理员 | ppClassVip 的 `admin` 角色 | 维护教师名单、查看课堂审计、配置配额；默认不进入课堂内容 |
| PPClassVip 后端 | Go 服务 | 教师登录、课堂状态机、聊天/附件、录屏分片、审计、限流、清理 |
| PPCDN 控制面 | ppcenter | 选 Anchor、签发 WHIP/WHEP 凭据、媒体就绪通知 |

### 2.2 一节标准课

```text
教师登录
  -> 创建课堂（即时课堂 / 预约课堂）
  -> 复制邀请链接发给学生
  -> 教师先进入，设备预检（摄像头/麦克风/扬声器/网络）
  -> 学生打开链接，输入昵称加入
  -> 双方 camera 腿建立，互拉
  -> 教师点“共享屏幕”，屏幕腿建立，学生自动切主画面
  -> 教师点“录屏” -> 学生收到录制确认 -> 学生同意后 MediaRecorder 开始
  -> 录屏画面为教师屏幕；音频混合教师麦克风 + 学生远端声音
  -> 课堂中：文字/图片/文件、静音、关摄像头、断线重连
  -> 教师结束课堂
  -> 录屏停止并保存到教师本地，媒体腿释放
  -> 课后：录屏文件保存在教师本机；游客学生不获得录像，除非教师自行分享文件
```

### 2.3 课堂状态机

```text
created --teacher join--> waiting --student join--> active --end--> ended
   |                        |                       |
   | invite expired         | teacher leave         | media error
   v                        v                       v
expired                  waiting/ended            failed
```

- `waiting`：教师已进入，学生未到；教师可以看到“等待学生加入”。
- `active`：至少双方媒体腿建立；允许一方短暂重连。
- `ended`：正常结束；保留聊天/文件，房间只读。
- `failed`：建立失败或异常结束；保留诊断信息，不生成“成功课堂”记录。
- 房间空闲超过配置时间（建议 30 分钟）自动回收；邀请链接默认 24 小时有效、一次性使用。

---

## 3. 总体架构

### 3.1 架构图

```text
┌──────────────────────────── 浏览器 ────────────────────────────┐
│ 教师端 / 学生端（ppclass-web，React + TypeScript）              │
│  ├─ ppclass SDK：REST + WS + 媒体编排                           │
│  ├─ pplayer：WHIP 推流 / WHEP 拉流 / H264 优先 / 断线重连       │
│  ├─ MediaRecorder：教师端录屏 + 麦克风/学生声音混音 + 分片上传  │
│  └─ 聊天、文件上传、屏幕共享、设备预检                          │
└───────────────┬───────────────────────────────┬────────────────┘
                │ HTTPS / WebSocket             │ WHIP / WHEP
                ▼                               ▼
┌──────────────────────────────┐      ┌──────────────────────────┐
│ ppClassVip（Go 服务）         │      │ mmx Anchor（Origin）      │
│  ├─ 教师 JWT / 游客邀请       │      │  ├─ 教师摄像头 leg        │
│  ├─ 课堂状态机 / WS           │      │  ├─ 学生摄像头 leg        │
│  ├─ 聊天 / 附件 / 录屏分片    │      │  └─ 屏幕共享 leg          │
│  ├─ MySQL 8                  │      │                          │
│  └─ 本地文件或对象存储        │      │ 媒体只做单跳转发          │
└───────────────┬──────────────┘      └──────────────────────────┘
                │ 内部 HTTP：选点 / 签发媒体凭据 / 就绪通知
                ▼
┌──────────────────────────────┐
│ ppcenter（PPCDN 控制面）      │
│  ├─ Anchor 调度               │
│  ├─ WHIP/WHEP 凭据            │
│  └─ 节点心跳 / 容量           │
└──────────────────────────────┘
```

### 3.2 与 PPCDN 的边界

| 层 | 负责 | 不负责 |
| --- | --- | --- |
| ppClassVip 后端 | 教师/游客、课堂、聊天、附件、录屏分片、审计 | 不承载媒体、不做视频编解码、不做视频合成/混音 |
| ppcenter 媒体编排 | 选 Anchor、签发 WHIP/WHEP 凭据、参与方就绪通知、释放资源 | 不转发媒体、不做混流、不解析 SDP 内容 |
| mmx Anchor | 单跳转发、WHEP 分发 | 不参与课堂录屏，不知道“课堂/教师/学生”业务语义 |
| ppclass-web | 采集、推流、拉流、教师端录屏、UI、重连 | 不持有 appSecret，不直连 mmx 管理端口 |
| 对象/文件存储 | 图片、文件、录屏文件 | 不参与鉴权决策 |

### 3.3 为什么 MVP 不做 P2P

- 1v1 教学的核心是“稳定上课 + 屏幕清晰 + 能回放”，不是极限延迟。中继模式端到端通常 200–400ms，教学互动足够。
- P2P 需要扩展当前非对称 P2P 协调器为对称会话、处理双 video track、TURN 兜底、计费口径和失败回退，工作量不小。
- 先做中继可以把风险集中在课堂业务本身；P2P 作为 P3 的成本优化，不影响业务协议。

---

## 4. 媒体面设计

### 4.1 媒体腿与流命名

一条“媒体腿”是一路独立推流/拉流。同一时刻最多三条活跃腿（屏幕共享只有一方）：

| 腿 | 推流方 | 内容 | 谁订阅 |
| --- | --- | --- | --- |
| `teacher-cam` | 教师 | 摄像头 + 麦克风（Opus） | 学生 |
| `student-cam` | 学生 | 摄像头 + 麦克风（Opus） | 教师 |
| `teacher-screen` | 教师 | 屏幕/窗口，视频 only | 学生 |
| `student-screen` | 学生（需授权） | 屏幕/窗口，视频 only | 教师 |

同一时刻只允许一条屏幕腿，因此 MVP 最多 3 路 publisher。

流命名继续遵守 mmx 的 `{appId}/{tableId}-{view}` 两段式，且 `tableId`、`view` 内都不含 `-`：

```text
{appId}/cls{roomId}-t     # 教师摄像头 + 麦克风
{appId}/cls{roomId}-s     # 学生摄像头 + 麦克风
{appId}/cls{roomId}-ts    # 教师屏幕，视频 only
{appId}/cls{roomId}-ss    # 学生屏幕，视频 only（可选）
```

- `roomId` 使用 base32/hex，不含 `-`；`tableId` 统一是 `cls{roomId}`，用 view 区分媒体腿。
- 课堂录屏不依赖这些 path：录制在教师浏览器内完成，Anchor 只负责转发。
- 视频统一 H264，音频统一 Opus；不启用 simulcast（1v1 不需要），屏幕共享也不做多档。

### 4.2 中继模式连接方式

```text
教师浏览器 ──WHIP publish──▶ Anchor: cls{roomId}-t
学生浏览器 ◀─WHEP read───── Anchor: cls{roomId}-t

学生浏览器 ──WHIP publish──▶ Anchor: cls{roomId}-s
教师浏览器 ◀─WHEP read───── Anchor: cls{roomId}-s

教师浏览器 ──WHIP publish──▶ Anchor: cls{roomId}-ts
学生浏览器 ◀─WHEP read───── Anchor: cls{roomId}-ts
```

- 教师端和学生端各维护：
  - 1 个摄像头发送 PC（WHIP，含音视频）；
  - 1 个对端摄像头接收 PC（WHEP）；
  - 屏幕共享时额外 1 个屏幕发送 PC + 1 个屏幕接收 PC。
- 屏幕共享不改变摄像头发送 PC，避免中途 renegotiation 影响正在进行的音视频。
- 屏幕共享停止时只关闭屏幕腿，摄像头腿继续。

### 4.3 屏幕共享

- 使用 `navigator.mediaDevices.getDisplayMedia()`，用户自行选择整个屏幕、窗口或标签页。
- 约束建议：`width ideal 1920`、`height ideal 1080`、`frameRate ideal 15`、`contentHint = "text"`；优先保持分辨率而不是流畅度。
- 屏幕腿只推视频；教师麦克风仍在摄像头腿，避免同一路音频重复。系统声音首版不共享；如需共享，P2 单独评估（浏览器支持差异大）。
- 教师端录屏复用同一个 `getDisplayMedia` 流：共享屏幕时可直接开始录屏；未共享时点“录屏”会先弹出屏幕选择框。
- 必须显式把 H264 放到 codec 首位，避免浏览器默认 VP8/VP9/AV1 导致 Origin/Edge 链路不兼容；复用 `ppwebpublish.mjs` 现有的 `preferH264` 逻辑。
- 权限：
  - 教师默认可共享；
  - 学生默认不可共享；教师点击“允许学生共享”后，学生才能拿到屏幕推流凭据；
  - 同一时刻只允许一条屏幕腿，后发起者收到 `screen_busy`。
- 隐私：
  - 共享前提示“学生将看到你选择的整个屏幕/窗口”；
  - 教师端显示明显的“正在共享”状态；
  - 不在日志中记录窗口标题、标签页 URL 等敏感信息。

### 4.4 编码、带宽与 QoS

| 媒体 | 编码 | 建议参数 |
| --- | --- | --- |
| 摄像头 | H264 | 720p/30 或 640×360/15；1–2 Mbps；弱网降到 360p/15 |
| 屏幕 | H264 | 1920×1080/15；1.5–2.5 Mbps；优先保分辨率 |
| 音频 | Opus | 32–48 kbps；`echoCancellation/noiseSuppression/autoGainControl` 全开 |

- 发送优先级：**音频 > 屏幕 > 摄像头**。弱网时先降摄像头分辨率/帧率，再降屏幕帧率，最后暂停摄像头视频但保留音频。
- 屏幕共享使用 `RTCRtpSender.setParameters()` 设置 `maxBitrate`，避免屏幕码率挤掉音频。
- 课堂开始前做一次网络探测（复用现有 NAT/延迟探测思路），给出“网络一般/较差”的提示；不因为探测结果直接拒绝上课。
- 关摄像头时不要断开 WHIP：用 canvas 生成黑色视频轨替换，麦克风继续；这样对端不会反复重连，录屏也不会断段。

### 4.5 教师端录屏与回放

**录制位置：教师浏览器。** 不使用网络侧 split-rec，不在服务端分别录多路再合成。

**视频：**

- 教师在课堂内点击“录屏”，浏览器调用 `getDisplayMedia()` 选择屏幕/窗口；如果已经在共享屏幕，直接复用当前屏幕流。
- 录制视频源就是教师屏幕，不加摄像头小窗；屏幕 1080p/15fps、1–2 Mbps，`contentHint="text"`。
- 教师停止共享屏幕时，录屏同步停止并保存到本地；UI 提前提示“停止共享将同时停止录屏”。

**音频：必须同时包含教师麦克风和学生远端声音。**

- 教师麦克风：`getUserMedia({audio:true})`，保留 `echoCancellation/noiseSuppression/autoGainControl`。
- 学生声音：从 WHEP 接收端拿到远端音频 `MediaStreamTrack`；`ppplayer` 需暴露 `remoteStream` / `onRemoteAudioTrack`。
- 用 Web Audio 混音：
  - `AudioContext.createMediaStreamSource(micStream)`；
  - `AudioContext.createMediaStreamSource(new MediaStream([studentAudioTrack]))`；
  - 两路都接到 `MediaStreamAudioDestinationNode`，得到一条混合音轨用于录制；
  - 课堂播放仍由 pplayer 的 `<video>` 输出学生声音；不要再把学生源接 `AudioContext.destination`，避免与 video 元素重复播放；
  - 若 pplayer 的音频输出被禁用/不可用，才把学生源接 `AudioContext.destination` 作为唯一播放路径；
  - 教师麦克风不要接扬声器，避免啸叫；`AudioContext` 在教师点击“录屏”后创建/`resume()`，避免自动播放策略导致静音。
- 录制流 = 屏幕视频轨 + Web Audio 混合音轨：
  ```js
  const mixed = new MediaStream([
    screenStream.getVideoTracks()[0],
    mixDestination.stream.getAudioTracks()[0],
  ]);
  const recorder = new MediaRecorder(mixed, { mimeType: "video/webm;codecs=vp9,opus" });
  ```
- 建议教师戴耳机。若用扬声器，学生声音会被麦克风二次拾取，导致录制里学生声音偏大/有回声；UI 必须提示。
- 学生远端音频经过 jitter buffer，可能比屏幕晚 100–300ms；首版接受这个偏差，不做服务端音画对齐。
- 学生加入/重连导致远端音轨变化时，重建混音源；短暂断线允许音频短暂空缺，超过 30 秒则暂停录制并提示，恢复不了就停止并标记 `interrupted`。
- 学生未加入、学生音频轨不可用、或学生拒绝录制时，不允许开始录屏（`student_audio_unavailable` / `recording_declined`）。

**录制确认：**

- 教师点“录屏” → `POST /recordings/start` 创建 `pending_consent` 会话 → WS 给学生发 `recording.request`。
- 学生点击“同意” → `POST /recordings/:rid/consent` → 服务端通知教师 `recording.consented` → 教师端才启动 `MediaRecorder`。
- 学生拒绝或 60 秒未确认 → 录制不开始，教师端提示。
- 学生可在课堂中要求停止录屏；教师端收到后停止并保存到本地。

**格式与本地保存：**

- 首版只交付 WebM：`video/webm;codecs=vp9,opus` → `video/webm;codecs=vp8,opus` → `video/webm`。不做 MP4。
- 不支持 WebM `MediaRecorder` 的浏览器（如部分 Safari）禁用教师端录屏，但不影响上课。
- 录制过程中 `MediaRecorder` 的 Blob 只保存在教师浏览器内存中；停止录制时合并为单个 Blob 并触发本地下载（`<a download>`）。
- 服务端不接收录像文件，也不保存分片。停止录制后调用 `POST /v1/classrooms/:id/recordings/:rid/stop` 结束会话并清理课堂录屏状态。
- 教师页面刷新/崩溃会丢失尚未保存的录制；UI 在录制中提示“不要关闭页面”。

**本地回放：**

- 录制产物是教师本机的单文件 WebM，可直接用浏览器或播放器打开，支持倍速、拖动。
- 录制画面只有教师屏幕，音频包含教师和学生；服务端不提供回放页或下载链接。
- 录制开关、录制中状态、学生确认状态都要在 UI 明确展示；录屏开始前必须完成学生确认（合规要求见 §8）。

### 4.6 P2P 演进（P3）

- 摄像头 + 麦克风可以走单个 `sendrecv` PeerConnection；屏幕作为第二条 video transceiver 或继续走中继。
- 复用现有 NAT 探测、P2P token、信令 WS、P2P 计费；扩展对称会话模型。
- 直连失败或质量差时无感回落到 Anchor 中继，与直播 P2P 的“直连优先、失败回退 Edge”一致。
- P2P 不作为 MVP 依赖；课堂协议不假设媒体一定经过 mmx，也不假设一定直连。

---

## 5. 课堂业务设计

### 5.1 房间生命周期

- **创建**：教师创建即时课堂，或从预约课程进入。服务端生成 `roomId`、`roomCode`、一次性邀请 token。
- **等待**：教师进入后状态 `waiting`，显示邀请链接/二维码；学生加入后转 `active`。
- **加入**：校验邀请 token（未过期、未使用、房间未结束），创建 participant 身份，返回 room token、媒体凭据、WS 地址。
- **重连**：room token 有效期内可重连；服务端恢复 participant 状态，重新签发媒体凭据；聊天用 `afterId` 补拉。
- **结束**：教师结束或双方离开；通知教师端停止录屏并 finalize、释放媒体凭据、关闭 WS、写审计；房间转只读。
- **回收**：空闲超时、邀请过期、异常房间由 reaper 定时清理；录屏/文件按保留期清理。

### 5.2 权限模型

| 能力 | 教师 | 学生 | 说明 |
| --- | --- | --- | --- |
| 创建/结束课堂 | 是 | 否 | 结束课堂立即停录屏 |
| 邀请/移除学生 | 是 | 否 | 移除后 room token 失效 |
| 摄像头/麦克风 | 是 | 是 | 可自行开关 |
| 共享屏幕 | 是 | 需授权 | 同一时刻一条屏幕腿 |
| 发送文字/图片/文件 | 是 | 是 | 教师可关闭学生聊天 |
| 开始/停止录屏 | 是 | 否 | 教师浏览器录制；开始前必须学生确认 |
| 录制确认/拒绝 | 否 | 是 | 学生拒绝则不能开始录制 |
| 静音学生 | 是 | 否 | 服务端只发控制消息，不强制关设备 |
| 查看课后回放 | 是 | 默认否 | 游客学生需教师单独分享回放链接 |

- 所有 REST/WS/媒体凭据都绑定 `roomId + userId/participantId + role + scope + exp`。
- 媒体凭据短 TTL（建议 ≤ 10 分钟），客户端在到期前续签；房间结束立即失效。
- 房间 ID 不可枚举；邀请 token 只存哈希，服务端不记录明文。

### 5.3 文字聊天

- 传输：课堂 WebSocket；持久化：MySQL `classroom_messages`。
- 消息类型：
  - `text`：纯文本，服务端转义/前端按文本渲染，禁止 HTML；
  - `image`：引用附件，前端显示缩略图/灯箱；
  - `file`：引用附件，显示文件名、大小、下载按钮；
  - `system`：加入/离开、屏幕共享开始/停止、录制开始/停止、教师控制。
- 顺序与幂等：
  - 客户端生成 `clientMsgId`，服务端在 `(room_id, sender_id, client_msg_id)` 上唯一；
  - 服务端写入后返回持久化 `id`，作为房间内排序游标；
  - 客户端按 `id` 排序，发送中显示 pending，ACK 后转 sent，超时可重试。
- 历史：
  - `GET /v1/classrooms/:id/messages?afterId=&beforeId=&limit=`；
  - 重连后从本地最后 `id` 补拉；
  - 课堂结束后只读，保留期内可查。
- 输入状态：`typing.start/stop` 仅走 WS，不持久化。
- 已读：1v1 场景可做 `readUpToId`，首版可选。
- 频率限制：每人每分钟消息数、图片/文件数限制；复用现有 `security_limiter`。
- 内容安全：长度上限（文本 2000 字），禁止 HTML/SVG；敏感词/投诉举报 P2 再做。

### 5.4 图片与文件

**设计原则：文件不塞进 WebSocket，也不走 DataChannel。**

- 上传：`POST /v1/classrooms/:id/attachments`，`multipart/form-data`，返回 `attachmentId`。
- 发送：上传成功后，客户端再发一条 `image`/`file` 消息引用 `attachmentId`。
- 下载：`GET /v1/classrooms/:id/attachments/:attachmentId/download?token=...`，短时签名 URL，支持 Range。
- 图片可直接在聊天流内预览；非图片文件一律 `Content-Disposition: attachment`，不在浏览器内联执行。
- 限制建议：

| 项 | MVP 建议 |
| --- | --- |
| 单张图片 | ≤ 10 MB；JPEG/PNG/GIF/WebP；校验魔数 + 解码尺寸 |
| 单个文件 | ≤ 50 MB；PDF/Office/zip/txt/图片等白名单 |
| 单课堂总量 | ≤ 500 MB |
| 文件名 | 服务端随机 key；原始文件名只作展示，不参与路径 |
| 危险类型 | 拒绝 exe/bat/cmd/js/html/svg 等可执行或可脚本类型 |
| 病毒扫描 | P2 接入 ClamAV；MVP 先做类型/魔数/大小限制 |
| 存储 | MVP 本地磁盘；生产建议对象存储 + 生命周期 |
| 保留期 | 与课堂资料策略一致，默认 1 年；到期删除并保留 tombstone |

- 图片如需去除 EXIF，P2 在上传后异步重编码；MVP 原样保存但只通过受控端点返回。
- 下载鉴权：签名 token 绑定 `attachmentId + roomId + userId + exp`；过期后重新申请。
- 断点续传：MVP 支持 Range 下载；上传失败由前端重试，P2 做分片/断点续传。
- 课后资料：课堂结束后附件仍可下载（保留期内）；教师可删除单条消息/附件。

### 5.5 通知与未读

- 课堂内：WS 实时推送新消息、参与者状态、屏幕共享状态、录屏状态。
- 课堂外：首版不做推送；教师可在课堂列表看到“学生已加入/已离开”。
- 未读：按 `lastReadMessageId` 计算；课堂结束后进入历史页时清零。

---

## 6. 控制面与数据模型

### 6.1 数据表（新增）

**`classroom_rooms`**

| 字段 | 说明 |
| --- | --- |
| `id` / `room_id` | 内部 ID / 对外短 ID |
| `lesson_id` | 关联课程/预约，可空 |
| `room_code` | 邀请短码 |
| `teacher_user_id` | 教师用户 |
| `student_user_id` | 学生用户，游客可空 |
| `state` | `created/waiting/active/ended/failed/expired` |
| `anchor_node_id` | 中继 Anchor |
| `recording_state` | `off/pending_consent/recording/finalizing/completed/declined/failed/interrupted` |
| `created_at/started_at/ended_at` | 时间 |
| `end_reason` | `teacher_end/student_left/timeout/media_error/network_error` |

**`classroom_participants`**

| 字段 | 说明 |
| --- | --- |
| `id` / `room_id` | 参与记录 |
| `user_id` | 登录用户，可空（游客） |
| `display_name` | 学生昵称/教师名称 |
| `role` | `teacher/student` |
| `permissions` | JSON：`screenShare/chat/upload` 等 |
| `joined_at/left_at` | 进出时间 |
| `last_seen_at` | 心跳 |

**`classroom_invites`**

| 字段 | 说明 |
| --- | --- |
| `id` / `room_id` | 邀请 |
| `token_hash` | 一次性 token 哈希 |
| `role` | 默认 `student` |
| `expires_at/used_at` | 过期/使用时间 |
| `created_by` | 教师 |

**`classroom_messages`**

| 字段 | 说明 |
| --- | --- |
| `id` | 自增，房间内排序游标 |
| `room_id` | 课堂 |
| `sender_id` | participant id |
| `sender_role` | `teacher/student/system` |
| `type` | `text/image/file/system` |
| `body` | 文本/系统消息 |
| `attachment_id` | 附件，可空 |
| `client_msg_id` | 客户端幂等 ID |
| `created_at/deleted_at` | 时间 |

唯一索引：`(room_id, sender_id, client_msg_id)`；查询索引：`(room_id, id)`。

**`classroom_attachments`**

| 字段 | 说明 |
| --- | --- |
| `id` / `room_id` | 附件 |
| `uploader_id` | 上传者 participant |
| `kind` | `image/file` |
| `file_key` | 服务端随机 key |
| `original_name/content_type/size` | 元数据 |
| `sha256` | 校验/去重 |
| `status` | `stored/purged/quarantined` |
| `created_at/purged_at` | 时间 |

**`classroom_recordings`**

| 字段 | 说明 |
| --- | --- |
| `id` / `room_id` / `lesson_id` | 课堂/课程 |
| `recording_id` | 对外录制会话 ID |
| `uploader_id` | 教师 participant |
| `includes_student_audio` | 固定 true；用于审计和录制提示 |
| `consent_at` | 学生确认录制的时间 |
| `consent_by` | 确认录制的学生 participant |
| `started_at/ended_at` | 教室内时间 |
| `status` | `pending_consent/recording/finalizing/completed/declined/failed/interrupted/purged` |
| `error` | 失败原因 |

PPCDN 的 `appId` 是服务级配置（`ppcdn.appId`），不进入课堂业务表；所有课堂共用同一个单机构媒体 app。

**`lessons`（P2）**

| 字段 | 说明 |
| --- | --- |
| `id/teacher_user_id/student_user_id` | 课程归属 |
| `title/subject` | 课程信息 |
| `scheduled_at/duration_minutes` | 排课 |
| `status` | `scheduled/cancelled/completed` |

### 6.2 REST API

沿用现有 v1 信封、鉴权中间件、限流和错误码风格。

```text
POST   /v1/classrooms                     # 教师创建课堂
POST   /v1/classrooms/:id/invite          # 生成/刷新邀请链接
POST   /v1/classrooms/:id/join            # 学生凭邀请加入；教师也可调用进入
POST   /v1/classrooms/:id/leave
POST   /v1/classrooms/:id/end             # 教师结束
GET    /v1/classrooms/:id                 # 房间状态
POST   /v1/classrooms/:id/screen/start    # 申请屏幕推流凭据
POST   /v1/classrooms/:id/screen/stop
POST   /v1/classrooms/:id/recordings/start       # 教师发起录屏，进入 pending_consent
POST   /v1/classrooms/:id/recordings/:rid/consent # 学生确认录制
POST   /v1/classrooms/:id/recordings/:rid/decline # 学生拒绝录制
GET    /v1/classrooms/:id/recordings/:rid        # 查询录制会话状态
POST   /v1/classrooms/:id/recordings/:rid/stop   # 结束录制会话（文件保存在教师本地）
GET    /v1/classrooms/:id/messages
POST   /v1/classrooms/:id/messages
POST   /v1/classrooms/:id/attachments
GET    /v1/classrooms/:id/attachments/:aid/download
GET    /api/v1/classrooms/:id/ws          # 课堂 WS，room token 走子协议
```

`join` 返回示例：

```json
{
  "roomId": 123,
  "role": "teacher",
  "roomToken": "...",
  "ws": {
    "path": "/api/v1/classrooms/123/ws",
    "protocolPrefix": "ppclass.token."
  },
  "media": {
    "publish": {
      "camera": { "url": "https://.../whip/...", "bearerToken": "..." }
    },
    "subscribe": {
      "teacher-cam": "https://.../whep/...",
      "student-cam": "https://.../whep/...",
      "teacher-screen": "https://.../whep/..."
    },
    "iceServers": []
  },
  "expiresAt": "2026-10-30T12:00:00Z"
}
```

- `screen/start` 返回屏幕腿的 `publish` 凭据，并通过 WS 通知对端 `peer.publishing {leg:"teacher-screen"}`。
- `recordings/start` 只允许教师；服务端校验学生已加入，客户端必须确认已拿到学生远端音频轨，拿不到则不发起 / 返回 `student_audio_unavailable`。返回 `recordingId`、`consentExpiresAt`，状态为 `pending_consent`。
- 学生调用 `recordings/:rid/consent` 后，服务端通过 WS 通知教师 `recording.consented`；教师端才启动 `MediaRecorder`。学生调用 `decline` 或 60 秒未确认则状态 `declined`。
- 服务端不接收录像文件、不落盘录像；录制在教师浏览器本地完成并在停止时直接下载保存。
- 停止录制后教师调用 `recordings/:rid/stop`，服务端结束会话、清理课堂录屏状态并广播 `recording.state`。
- 错误码建议：`invalid_request`、`room_not_found`、`room_forbidden`、`room_ended`、`invite_expired`、`invite_used`、`room_full`、`screen_busy`、`screen_forbidden`、`recording_forbidden`、`recording_disabled`、`recording_declined`、`student_audio_unavailable`、`attachment_too_large`、`attachment_type_forbidden`、`no_anchor_available`、`account_in_arrears`。

### 6.3 WebSocket 协议

当前实现：`GET /api/v1/classrooms/:id/ws`，浏览器用 `Sec-WebSocket-Protocol: ppclass.token.<roomToken>` 传 token；所有事件统一为 `{type, data}`。P1 已实现 `room.state`、`presence.*`、`chat.*`、`typing`、`screen.*`、`recording.*`、`peer.publishing/unpublished`、`media.ready/stats`、`control.*`、`room.ended`、`ping/pong`、`error`。

客户端 -> 服务端：

```text
room.ping                 {}
chat.send                 { clientMsgId, type, body?, attachmentId? }
chat.read                 { upToId }
typing.start/stop         {}
screen.start.request      {}
screen.stop               {}
recording.stop.request    {}
media.ready               { leg }
media.stats               { rttMs, lossPct, jitterMs, bitrateBps }
```

服务端 -> 客户端：

```text
room.state                { state, participants }
room.joined               { participant }
room.left                 { participantId, reason }
chat.message              { id, clientMsgId, sender, type, body, attachment, createdAt }
chat.ack                  { clientMsgId, id, createdAt }
chat.read                 { participantId, upToId }
typing                    { participantId, active }
peer.publishing           { leg, streamPath }
peer.unpublished          { leg }
screen.state              { owner, active }
recording.request         { recordingId, teacherName }
recording.consented       { recordingId }
recording.declined        { recordingId, reason }
recording.stop.requested  { participantId }
recording.state           { state, startedAt, includesStudentAudio }
control.mute              { participantId, muted }
control.screen.permission { granted }
room.ended                { reason }
error                     { code, message }
```

- `chat.send` 持久化成功才返回 `chat.ack`；失败返回 `error`，客户端保留 pending 可重试。
- `peer.publishing` 由 ppClassVip 根据 ppcenter 的在线 pipeline 状态（内部 API 查询或回调）触发，客户端收到后再 WHEP 拉流，避免 404 轮询。
- `media.stats` 可用于课堂质量面板和课后报告，不记录 SDP/内网候选。

### 6.4 媒体凭据与选点

- ppClassVip 调 ppcenter 的内部媒体 API：选一个 Origin 作为 Anchor，同时具备 WHIP 接入与 WHEP 分发能力。
- 选点维度：同大区优先、到双方延迟较低、剩余容量最多、当前课堂数未超上限。
- 一次课堂在 Anchor 上占 3 路 publisher + 3 路 reader；需要给课堂单独计数，避免挤占直播容量。
- 教师摄像头/学生摄像头凭据在 `join` 时由 ppClassVip 向 ppcenter 申请；屏幕凭据在 `screen/start` 时申请；TTL 短，房间结束即失效。
- 如果 ppcenter 还没有课堂/通话级内部 API，先在 ppClassVip 内封装 `internal/ppcdn` 客户端，调用现有 publish/play 签发能力，后续再收敛为专用接口。

---

## 7. 客户端设计

### 7.1 技术栈

- **新前端 `ppclass-web`**：React 19 + TypeScript + Vite，独立站点（例如 `class.pp-cdn.org`），不塞进现有 console。
- **SDK `ppclass`**：框架无关的 TS 模块，封装 REST、WS、媒体编排、重连、聊天、上传；React 层只做 UI。
- **复用 pplayer**：
  - `ppwebpublish.mjs`：摄像头 WHIP 推流、H264 优先、token 刷新；
  - `ppplayer.mjs`：WHEP 拉流、Trickle ICE、ABR/缓冲；
  - `nat-probe.mjs` / `time-sync.mjs`：网络探测和延迟测量。
- **需要新增的 pplayer 能力**：
  - `ppwebpublish.mjs` 增加 `capture: "camera" | "screen"`，屏幕模式使用 `getDisplayMedia`；
  - 屏幕腿音频关闭、`contentHint="text"`、分辨率优先；
  - 摄像头关闭时用 canvas 黑帧替换，不断开 WHIP；
  - `ppplayer.mjs` 暴露 `remoteStream` / `onRemoteAudioTrack`，供录屏混音使用；
  - 新增 `ppscreenrecorder.mjs`：屏幕视频 + Web Audio 混音（教师麦克风 + 学生远端音频）、WebM 分片、上传/续传/finalize；
  - 通过 `MediaRecorder.isTypeSupported()` 只选择 `video/webm;codecs=vp9,opus` / `vp8,opus` / `video/webm`；不支持则禁用录屏；
  - 不实现教师摄像头小窗合成。

### 7.2 页面与 UI

**教师端**

- 创建课堂：即时课堂 / 从预约课程进入；生成邀请链接和二维码。
- 课前预检：摄像头预览、麦克风电平、扬声器测试、网络质量、浏览器兼容提示。
- 课堂主界面：
  - 主画面：屏幕共享（有共享时）或学生摄像头；
  - 小窗：教师自己、学生；
  - 工具栏：麦克风、摄像头、共享屏幕、录屏、邀请、结束课堂；
  - 右侧面板：聊天 / 文件 / 课堂信息；
  - 状态条：网络、延迟、录屏中、屏幕共享中、分片上传状态。
- 课堂控制：静音学生、允许学生共享屏幕、关闭学生聊天、移出学生。
- 录屏时显示“录制中，请勿关闭页面”；提示教师戴耳机，避免学生声音被扬声器二次拾取。
- 点击录屏后显示“等待学生确认”；学生拒绝/超时则恢复未录制状态。
- 结束课堂前提示先停止录屏并等待 finalize。

**学生端**

- 邀请页：显示教师/课程信息，输入昵称加入；设备预检。
- 课堂主界面：教师屏幕/摄像头主画面，自己小窗，聊天/文件面板，举手/申请共享屏幕（P2）。
- 教师发起录屏时弹窗说明“将录制教师屏幕、教师麦克风和你的声音”，学生选择同意/拒绝；拒绝后教师不能录制。
- 录屏中持续显示“正在录制（含你的声音）”；学生可点“要求停止录制”。
- 游客学生默认没有课后回放入口，教师另行分享链接时才能看。

### 7.3 弱网与重连

- WS 断开：指数退避重连（1s/2s/5s/10s），期间显示“正在重连”，聊天 pending 保留。
- 媒体断开：先 ICE restart；失败则重新申请媒体凭据、重建 PC；屏幕腿单独恢复，不影响摄像头。
- 网络切换（Wi-Fi/蜂窝）：监听 `iceconnectionstate`，触发重连；提示用户网络变化。
- 页面刷新：room token 有效则自动重新加入；聊天按 `afterId` 补拉；录屏会话中断则标记 `interrupted`。
- 录屏分片上传失败：前端指数退避重试，未确认分片留在内存/IndexedDB；恢复后先查 `lastChunkSeq` 再续传。
- 学生远端音频轨丢失/重连：重建 Web Audio 混音源；30 秒内恢复则继续，超过则暂停录屏并提示，无法恢复则停止并标记 `interrupted`。
- 设备被占用/权限拒绝：给出明确提示和“重试/切换设备”入口；允许音频 only 继续上课。
- 重复加入：同一账号同一房间只保留一个活跃 participant，旧连接被踢下线并提示。

### 7.4 兼容性

- 教师端优先 Chrome/Edge 最新版；Safari 作为学生端兼容目标。
- 教师端录屏要求 `MediaRecorder` 支持 WebM（Chrome/Edge/Firefox）；Safari 教师端可能只能上课、不能录屏，UI 明确禁用录屏按钮。
- 回放交付 WebM；Chrome/Edge/Firefox 可直接播放，Safari 播放 WebM 的能力因版本而异，必要时提供下载。
- 移动浏览器首版只保证观看/聊天；推流、屏幕共享和录屏以桌面为主。
- 不支持 WebRTC、H264、`getDisplayMedia` 或 `MediaRecorder` 时，进入前明确提示；教师端不支持录屏则禁用录屏按钮，不影响上课。

---

## 8. 安全与合规

| 项 | 设计 |
| --- | --- |
| 教师鉴权 | ppClassVip 自己的用户表 + JWT；课堂创建校验 `teacher/admin` 角色 |
| 学生鉴权 | 一次性邀请 token；游客 participant 绑定 room token；正式学生 P2 接入 |
| 房间隔离 | 所有接口校验 `roomId + participantId + role + scope`；不能跨房间读写 |
| 媒体鉴权 | WHIP/WHEP 短 TTL 凭据；屏幕凭据单独签发；房间结束立即失效 |
| 传输安全 | 全站 HTTPS/WSS；媒体 DTLS-SRTP |
| 中继可见性 | Anchor 节点可见媒体明文；产品隐私政策必须说明；E2EE 仅 P2P 阶段评估 |
| 文件安全 | 魔数校验、类型白名单、随机 key、签名下载、`Content-Disposition: attachment`；P2 病毒扫描 |
| XSS | 聊天只按纯文本渲染；图片走受控端点；禁止 SVG/HTML 上传 |
| 滥用 | 邀请尝试限速、聊天/上传限速、每教师并发课堂上限、每课堂存储配额 |
| 审计 | 记录创建/加入/离开/屏幕共享/录屏/文件上传下载/删除/结束；不记录消息正文和 SDP |
| 录制内容 | 教师端录屏：教师屏幕 + 教师麦克风 + 学生远端声音；不含教师摄像头小窗、不含学生画面 |
| 学生同意 | 录屏开始前学生必须明确同意；拒绝则不能开始；学生可在课堂中要求停止 |
| 隐私 | 课堂内显示录制中（含学生声音）；教师建议戴耳机；支持配置保留期和删除；未成年人场景需监护人同意 |
| 数据保留 | 聊天/文件默认 1 年，录屏默认 90 天（可配置）；到期 reaper 删除，保留 tombstone |
| 数据位置 | 媒体节点、对象存储、数据库的区域需可配置；跨境教学需确认数据驻留要求 |

---

## 9. 计费与配额

- **媒体流量**：沿用现有 app 流量计费；课堂的 3 路 publisher + 3 路 reader 都计入 Anchor/Edge 流量。
- **录屏存储**：按教师端上传的单文件大小和保留时长计费；对象存储生命周期与 `classroom_recordings` 对齐。
- **文件存储**：按课堂附件总量计费；超过配额拒绝上传，不影响上课。
- **课堂时长**：`classroom_rooms` 记录开始/结束，可用于按分钟计费或统计。
- **P2P（P3）**：复用现有 `chargeRealtimeP2PUsage`，按 session 秒数计费，降低成本。
- **欠费**：创建课堂前检查 `IsInArrears`；已开始的课堂不立即断媒体，给宽限期并提示；新课堂/新录屏拒绝。
- **配额建议**：
  - 每教师并发课堂数：1–3；
  - 每 app 并发课堂数：按 Anchor 容量配置；
  - 单课堂文件总量：500 MB；
  - 单课堂录屏时长：可配置上限（如 3 小时）。

---

## 10. 可观测性与 SLO

| 指标 | 维度 | 来源 |
| --- | --- | --- |
| 课堂创建数 / 活跃课堂数 | region | ppClassVip |
| 加入成功率、加入失败原因 | app/browser/region | join API + WS |
| 加入→首帧时延 | P50/P95/P99 | 客户端 `media.ready` + 首帧回调 |
| 屏幕共享开始时延、失败率 | browser | screen API + 客户端 |
| 课堂 RTT/丢包/抖动/码率 | 每课堂/每腿 | 客户端 `getStats()` -> `media.stats` |
| 聊天发送成功率、P95 延迟 | app | WS ack |
| 文件上传成功率、平均耗时 | 类型/大小 | 上传 API |
| 录屏成功率、分片上传失败率、finalize 失败率 | browser | `classroom_recordings` |
| 学生音频混音失败/音轨丢失次数 | browser | 客户端埋点 |
| Anchor 课堂容量水位 | node | node_manager |
| 异常结束原因分布 | app | `classroom_rooms.end_reason` |

SLO 建议：

- 加入成功率 > 99%（双方在线、浏览器支持）。
- 加入→首帧 P95 < 1.5s；屏幕共享开始 P95 < 2s。
- 聊天送达 P95 < 300ms；文件上传成功率 > 99.5%。
- 中继媒体端到端延迟 P95 < 400ms。
- 教师端录屏成功率 > 99%；停止后 10 分钟内可回放（取决于文件大小和上传带宽）。
- 录屏抽检必须能听到教师和学生双方声音；缺少学生声音的录制不得标记 `completed`。
- 课堂服务可用性 99.9%。

---

## 11. 失败矩阵

| 故障 | 系统行为 |
| --- | --- |
| 邀请过期/已使用 | 返回 `invite_expired/invite_used`，提示教师重新生成 |
| 学生未到 | 教师停留在 `waiting`，可继续等待或结束 |
| 教师未到 | 学生不能开始；显示“等待教师” |
| 摄像头/麦克风权限拒绝 | 允许音频 only 或纯观看；提供重试和浏览器设置指引 |
| 屏幕共享被用户取消 | 不报错，保持摄像头腿；UI 恢复“未共享” |
| 屏幕共享浏览器不支持 | 提示改用 Chrome/Edge；学生仍可看教师共享 |
| 同一时刻两人共享 | 后发起者收到 `screen_busy`，教师可强制停止当前共享 |
| 弱网/丢包 | 先降摄像头，再降屏幕帧率；音频优先；显示网络状态 |
| WS 断开 | 宽限期重连；聊天 pending 保留；超时按离开处理 |
| 媒体 ICE 失败 | ICE restart；失败重建 PC；再失败提示网络问题 |
| Anchor 故障 | 课堂中断；提示重新进入；P2 可在备用 Anchor 重建 |
| 学生拒绝录制/60 秒未确认 | 录制不开始，状态 `declined`；课堂继续 |
| 学生音频轨不可用 | 不允许开始录屏，返回 `student_audio_unavailable` |
| 学生音频轨中途丢失 | 重建混音；30 秒未恢复则暂停/停止录制并提示 |
| 教师端录屏失败/浏览器崩溃 | 课堂继续；已上传分片保留，录制标记 `interrupted`，能播放的部分可下载 |
| 教师浏览器不支持 WebM MediaRecorder | 禁用录屏按钮；课堂继续 |
| 录屏分片上传失败 | 前端重试并续传；长时间失败提示教师检查网络，不阻塞上课 |
| 文件上传失败 | 消息保留 pending，可重试；不产生半条消息 |
| 存储满/配额超限 | 拒绝新上传/新录屏，提示清理；不影响音视频 |
| 重复加入 | 旧连接被替换并提示“账号在其他窗口加入” |
| ppClassVip 不可用 | 已建立媒体可短暂继续；聊天/控制/录屏上传/结束不可用；新加入失败并退避 |
| ppcenter 不可用 | 已建立媒体可短暂继续；新课堂无法选 Anchor/签发凭据；恢复后重试 |

---

## 12. 实施计划

### P0：技术验证（约 1 周）

1. 用两个浏览器 + 一个 Anchor 验证教师/学生摄像头互推互拉。
2. 验证屏幕共享独立 WHIP 腿 + 对端 WHEP 拉流。
3. 验证 `ppwebpublish.mjs` 的 `getDisplayMedia` 扩展和 H264 优先。
4. 验证教师端 `MediaRecorder` 录屏：屏幕 + Web Audio 混音（教师麦克风 + 学生远端音频）、无摄像头小窗、5 秒分片上传、finalize 后单文件 WebM 可播放且双方声音都在。
5. 验证课堂 WS 的鉴权、聊天 ack、断线补拉。
6. 验证 50 MB 文件上传/下载、Range 下载、类型拦截。

**产出**：一个可运行的双人 demo、教师端录屏样本、P0 结论和风险清单。

### P1：MVP（约 4–5 周）

1. ppClassVip：`classroom_rooms/participants/invites/messages/attachments/recordings` 模型 + 迁移。
2. ppClassVip：创建/邀请/加入/离开/结束、屏幕凭据、媒体凭据、Anchor 选点；封装 `internal/ppcdn`。
3. ppClassVip：课堂 WS（presence/chat/screen/control/media stats）、限流、审计。
4. ppClassVip：附件上传/下载、图片校验、录屏分片接收/finalize/存储、学生录制同意、保留期 reaper。
5. ppclass-web：教师创建/邀请/课堂页；学生邀请页/课堂页；学生录制确认弹窗。
6. ppclass SDK + `ppscreenrecorder.mjs`：媒体编排、聊天、上传、屏幕+双人音频录屏、重连。
7. 基础监控、错误码、日志脱敏。

**验收**：教师和学生能完整上一节课；摄像头、屏幕、聊天、图片、文件都可用；教师端录屏生成单文件且包含双方声音；断线可恢复。

### P2：教学闭环（约 3–4 周）

1. 课程/排课/预约（`lessons`）、课堂历史。
2. 录屏回放页、下载权限、回放分享链接、录屏保留期。
3. 教师控制：静音、允许共享、关闭聊天、移出学生。
4. 文件预览、课后资料、聊天导出。
5. 审计页/管理配置：并发上限、文件上限、保留期、录屏开关。
6. 兼容性测试、弱网测试、安全测试。

**验收**：真实教师可排课、上课、课后回放；管理员可查审计和配额。

### P3：增强（约 4–6 周，按需）

1. P2P 直连（摄像头/音频优先，屏幕可继续中继）。
2. 白板/标注、举手、课堂小工具。
3. 课程支付、作业、课后反馈。
4. 移动端 Web 优化 / 原生 App。
5. E2EE、AI 转写/摘要、移动端优化。

### 估算

- 2 名后端 + 1 名前端 + 兼职 QA：P0+P1 约 6 周，P2 约 3–4 周，生产加固 1–2 周。
- 单人全栈：P0+P1 约 10–12 周，P2 另加 6–8 周。
- 以上不含支付、移动原生、白板、AI 转写等 P3 项。

---

## 13. 验收基线

- **TEACH-ACC1**：教师创建课堂、生成邀请；学生凭链接加入，双方摄像头/麦克风互通。
- **TEACH-ACC2**：教师共享屏幕后学生 2 秒内看到画面；停止共享后学生恢复摄像头主画面。
- **TEACH-ACC3**：文字、图片、文件均能发送、持久化、重连后补拉；重复发送不产生重复消息。
- **TEACH-ACC4**：未授权用户无法加入/拉流/上传；邀请 token 不能复用；房间结束后媒体凭据失效。
- **TEACH-ACC5**：断网 10 秒内恢复后能继续上课；屏幕腿可单独恢复，不影响音频。
- **TEACH-ACC6**：学生确认后，教师端录屏生成单个 WebM，内容包含教师屏幕、教师麦克风和学生声音，不含教师摄像头小窗；回放页可直接播放并听到双方声音。
- **TEACH-ACC7**：课堂结束后聊天/文件/录屏在保留期内可访问，过期后被清理；游客学生默认不能看回放。
- **TEACH-ACC8**：50 个并发课堂（测试环境）加入失败率 < 1%，Anchor 容量告警可触发。
- **TEACH-ACC9**：日志不含消息正文、SDP 全文、内网候选、邀请 token 明文。

---

## 14. 开放问题

| 编号 | 问题 | 建议 |
| --- | --- | --- |
| TEACH-Q1 | 单机构部署 | 已确认：单个机构、单个课堂 app，不做多租户 |
| TEACH-Q2 | 学生是否允许游客加入 | 已确认：允许游客，一次性邀请链接 + 昵称加入 |
| TEACH-Q3 | 录屏是否包含教师摄像头小窗？ | 已确认：不包含，只录教师屏幕 |
| TEACH-Q4 | 录制保留多久？是否需要家长/机构下载？ | 默认 90 天，可配置；下载权限单独控制 |
| TEACH-Q5 | 是否需要课程支付/分账？ | P3；不复用 PPCDN 流量钱包做课程收款 |
| TEACH-Q6 | 是否需要白板/作业？ | P3；首版聊天+屏幕共享已能覆盖大部分 1v1 教学 |
| TEACH-Q7 | 是否要求 E2EE？ | 中继不 E2E；P2P 阶段评估 Insertable Streams |
| TEACH-Q8 | 是否支持跨大区上课？ | MVP 同区优先；跨区只保证可用，不承诺低延迟 |
| TEACH-Q9 | 是否需要移动原生 App？ | 先 Web；移动端观看/聊天优先，推流二期 |
| TEACH-Q10 | 课堂数据保留与未成年人合规要求？ | 上线前由业务确认；录屏前双方确认，提供删除/导出 |
| TEACH-Q11 | 录屏交付格式 WebM 是否可接受？ | 已确认：WebM 可接受，不做 MP4、不做服务端转封装 |

---

## 15. 关联文件与现状索引

> 更新（2026-10-31）：P1 已落地。后端包含附件、录屏分片/确认、屏幕共享、媒体凭据（`internal/ppcdn`）、WS 扩展、限流、审计与 reaper；前端 `ppclass-web/` 包含教师工作台、邀请页、课堂页、`ppclass` SDK 与 `ppscreenrecorder` 录屏模块。pplayer 通过 `sync:pplayer` 拷贝复用，未修改外部仓库。

**ppClassVip 后端（已落地骨架）**

- `cmd/server/main.go`：HTTP 服务入口、配置加载、MySQL、JWT、优雅退出。
- `cmd/seed/main.go`：创建/更新教师或管理员账号。
- `internal/config/config.go`：YAML + 环境变量配置。
- `internal/database/mysql.go`：GORM/MySQL 连接池、健康检查、AutoMigrate。
- `internal/auth/`：bcrypt 密码、JWT 签发/校验。
- `internal/model/user.go`：教师/管理员账号模型。
- `internal/store/user_store.go`：用户查询。
- `internal/middleware/`：请求 ID、访问日志、panic 恢复、CORS、JWT 鉴权。
- `internal/api/`：路由、登录、`/auth/me`、健康检查、统一响应。
- `conf/config.dev.yaml`、`.env.example`、`docker-compose.yml`、`Makefile`、`README.md`。

**ppClassVip 已新增（后端 P1）**

- `internal/model/classroom.go`：房间/参与者/邀请/消息/附件/录屏/审计/媒体统计模型。
- `internal/store/classroom_store.go`、`attachment_store.go`、`recording_store.go`、`audit_store.go`、`media_stat_store.go`。
- `internal/api/classroom*.go`：课堂 REST、WS、附件、录屏、屏幕、控制、审计。
- `internal/reaper/classroom_reaper.go`：过期房间/待确认录屏/中断录屏/附件清理。
- `internal/ppcdn/client.go`：调用 ppcenter publish/play 签发 WHIP/WHEP 凭据。
- `internal/storage/local.go`：本地附件与录屏分片存储。

**PPCDN 依赖（只读复用，不改业务）**

- 选点/容量：`ppcenter/internal/manager/node_manager.go`。
- WHIP 签发：`ppcenter/internal/apis/publish_v1.go`、`internal/utils/whip_token.go`。
- WHEP 签发：`ppcenter/internal/apis/player_v1.go`、`internal/services/stream.go`。
- 在线 pipeline：`node_manager.go` 的 `PipelineSet` / `GetActiveStreams`。
- 课堂录屏不使用 split-rec：`POST /api/record/split` / `record_proxy.go` 保持现状，不接入课堂。

**pplayer（复用方式）**

- 不修改外部 `pplayer` 仓库；`ppclass-web/scripts/sync-pplayer.mjs` 把 `ppwebpublish.mjs`、`ppplayer.mjs`、`p2p-answerer.mjs`、`nat-probe.mjs` 拷贝到 `public/vendor/pplayer/`。
- 摄像头/屏幕 WHIP 使用 `whipPublish`；WHEP 拉流使用 `MediaMTXWebRTCReader`。
- 录屏混音在 `ppclass-web/src/sdk/ppscreenrecorder.ts` 内完成，远端音频来自 WHEP reader 的 `onTrack`。

**前端（已新增）**

- `ppclass-web/`：教师登录/工作台、学生邀请页、课堂页、录屏记录弹窗。
- `ppclass` SDK：`src/sdk/` 下的 REST、WebSocket、媒体和录屏封装。

**部署**

- 服务：`ppclassvip` Go 二进制 + systemd；MySQL 8 独立库 `ppclassvip`。
- nginx：课堂站点、`client_max_body_size 64m`、WS 升级、上传超时。
- 存储：`/opt/ppclassvip/data/classroom-files`（附件），或对象存储桶 + 生命周期；录屏文件保存在教师本机，服务端不存储。
- 配置：`PPCLASS_MYSQL_DSN`、`PPCLASS_JWT_SECRET`、`PPCLASS_HTTP_ADDR`、`ppcdn.baseUrl/appId/appSecret`、`classroom.maxFileBytes`、`classroom.retentionDays`。
- 监控：课堂创建/加入失败/首帧/屏幕共享/上传/录屏指标与告警。
