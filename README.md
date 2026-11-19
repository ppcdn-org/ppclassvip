# PPClassVip

单机构 1v1 在线教学系统，基于PPCDN（www.pp-cdn.org）实现。业务面是独立的 Go 服务，负责教师/游客、课堂、聊天、文件、教师端本地录屏。

当前版本已包含基础框架和后端 P1 课堂闭环：

- Go 1.26 + Gin + GORM + MySQL 8
- YAML 配置 + 环境变量覆盖
- MySQL 连接池、AutoMigrate、健康检查
- 结构化日志（text/json）、请求 ID、访问日志、panic 恢复
- 统一响应信封、CORS
- 教师/管理员账号模型、JWT 登录、`/auth/me`
- 课堂房间、邀请链接、游客加入、教师加入、离开、结束课堂
- 课堂文字/图片/文件消息 REST、`clientMsgId` 幂等、消息游标分页
- 课堂 WebSocket：presence、聊天、输入状态、已读、屏幕共享、录屏确认、媒体状态
- 附件上传/下载：类型与魔数白名单、大小/课堂配额、签名鉴权、Range 下载
- 屏幕共享凭据与占用控制、教师控制（静音、允许学生共享、关闭聊天）
- 教师端录屏：学生同意后在教师浏览器本地录制并保存为 WebM 文件，服务端不存储、不提供回放
- PPCDN 媒体凭据：封装 `internal/ppcdn`，调用 ppcenter 的 publish/play 签发接口
- 限流、审计日志、日志脱敏、过期房间/邀请/附件清理 reaper
- Windows 远程桌面 Agent：学生授权、只读/控制、断线重连与自动更新
- 独立管理后台：用户、课堂、附件、审计与系统设置
- 种子用户命令、MySQL 8 本地 compose

## 目录

```text
cmd/
  server/            HTTP 服务入口
  seed/              创建/更新教师或管理员
  agent/             Windows 远程桌面 Agent
  agent-installer/   Agent 安装器
internal/
  api/               路由、登录、健康检查、响应
  auth/              密码哈希、JWT
  config/            YAML + 环境变量配置
  database/          MySQL/GORM 连接
  logger/            slog 初始化
  middleware/        请求 ID、日志、恢复、CORS、鉴权、限流
  model/             GORM 模型
  ppcdn/             ppcenter 媒体凭据客户端
  realtime/          WebSocket Hub 与远程控制通道
  reaper/            过期课堂/邀请/附件清理
  settings/          课堂配额与限流动态配置
  storage/           本地附件存储
  store/             数据访问
  version/           构建版本
configs/config.yaml  本地开发配置
docs/roadmap/        产品与技术规划
ppclass-web/         教师/学生课堂前端（React + Vite）
ppclass-admin/       管理后台前端（React + Vite）
```

## 快速开始

### 1. 启动 MySQL 8

有 Docker：

```bash
docker compose up -d mysql
```

没有 Docker 时，使用本机 MySQL 8 创建数据库：

```sql
CREATE DATABASE ppclassvip DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
CREATE USER 'ppclassvip'@'%' IDENTIFIED WITH mysql_native_password BY 'CHANGE_ME';
GRANT ALL PRIVILEGES ON ppclassvip.* TO 'ppclassvip'@'%';
FLUSH PRIVILEGES;
```

把 `CHANGE_ME` 换成你的数据库密码，然后写入 `.env` 的 `PPCLASS_MYSQL_DSN`。

### 2. 配置

默认配置在 `configs/config.yaml`。本地可以复制环境变量样例：

```powershell
Copy-Item .env.example .env
```

服务启动时会自动读取 `.env`（如果存在），环境变量优先级高于 YAML。

生产环境必须设置至少 32 位的 `PPCLASS_JWT_SECRET`，并设置 `PPCLASS_ENV=prod`。

### 3. 启动服务

开发时可以直接从仓库根目录启动：

```bash
go run ./cmd/server -config configs/config.yaml
```

或：

```bash
make run
```

本机也可以使用 `bin/` 作为运行目录（`.env`、`conf/`、`logs/`、`data/` 都在该目录下）：

```text
bin/
  ppclassvip.exe
  ppclassseed.exe
  .env
  conf/config.local.yaml
  logs/
  data/
```

```powershell
cd bin
.\ppclassvip.exe -config conf/config.local.yaml
```

`dataDir: ./data`、`.env` 都是相对 `bin/` 解析；stdout/stderr 需要重定向到 `bin/logs/`（应用本身不写文件日志）。

检查：

```bash
curl http://localhost:18080/healthz
curl http://localhost:18080/api/v1/ping
```

### 4. 创建教师账号

```bash
go run ./cmd/seed \
  -config configs/config.yaml \
  -email teacher@example.com \
  -password "change-me-123" \
  -nickname "Teacher" \
  -role teacher
```

或：

```bash
make seed EMAIL=teacher@example.com PASSWORD=change-me-123 NICKNAME=Teacher ROLE=teacher
```

`bin/` 运行目录中使用 `ppclassseed.exe`：

```powershell
cd bin
.\ppclassseed.exe -config conf/config.local.yaml -email teacher@example.com -password "change-me-123" -nickname "Teacher" -role teacher
```

### 5. 登录

```bash
curl -X POST http://localhost:18080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d "{\"email\":\"teacher@example.com\",\"password\":\"change-me-123\"}"
```

带 token 查询当前用户：

```bash
curl http://localhost:18080/api/v1/auth/me \
  -H "Authorization: Bearer <token>"
```

## 当前 API

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/healthz` | 健康检查，含数据库状态 |
| GET | `/readyz` | 就绪检查 |
| GET | `/api/v1/ping` | 存活探针 |
| POST | `/api/v1/auth/login` | 教师/管理员登录 |
| GET | `/api/v1/auth/me` | 当前登录用户 |
| POST | `/api/v1/auth/change-password` | 修改当前用户密码 |
| POST | `/api/v1/agent/pair` | Agent 换取远程桌面连接凭据 |
| POST | `/api/v1/agent/stop` | Agent 主动停止远程桌面会话 |
| POST | `/api/v1/classrooms` | 教师创建课堂 |
| GET | `/api/v1/classrooms` | 教师课堂列表 |
| POST | `/api/v1/classrooms/:id/invite` | 生成学生邀请 token |
| POST | `/api/v1/classrooms/:id/join` | 教师进入课堂，返回 room token |
| POST | `/api/v1/classrooms/join` | 游客凭邀请加入 |
| POST | `/api/v1/classrooms/:id/leave` | 参与者离开课堂 |
| GET | `/api/v1/classrooms/:id` | 课堂状态与参与者 |
| POST | `/api/v1/classrooms/:id/messages` | 发送文本/图片/文件消息 |
| GET | `/api/v1/classrooms/:id/messages` | 消息列表，支持 `afterId` / `beforeId` / `limit` |
| GET | `/api/v1/classrooms/:id/ws` | 课堂 WebSocket，room token 走子协议 |
| POST | `/api/v1/classrooms/:id/end` | 教师结束课堂 |
| DELETE | `/api/v1/classrooms/:id` | 删除课堂 |
| POST | `/api/v1/classrooms/:id/media/refresh` | 续签摄像头/屏幕媒体凭据 |
| POST | `/api/v1/classrooms/:id/screen/start` | 申请屏幕共享推流凭据 |
| POST | `/api/v1/classrooms/:id/screen/stop` | 停止屏幕共享 |
| POST | `/api/v1/classrooms/:id/control` | 教师控制：静音/屏幕权限/聊天 |
| GET | `/api/v1/classrooms/:id/audit` | 教师查看课堂审计 |
| POST | `/api/v1/classrooms/:id/remote-desktop/request` | 教师发起远程桌面申请 |
| POST | `/api/v1/classrooms/:id/remote-desktop/:sid/consent` | 学生同意远程桌面 |
| POST | `/api/v1/classrooms/:id/remote-desktop/:sid/decline` | 学生拒绝远程桌面 |
| POST | `/api/v1/classrooms/:id/remote-desktop/:sid/stop` | 停止远程桌面会话 |
| GET | `/api/v1/classrooms/:id/remote-desktop/:sid` | 查询远程桌面会话 |
| GET | `/api/v1/classrooms/:id/remote-desktop/:sid/control` | 远程控制 WebSocket |
| POST | `/api/v1/classrooms/:id/attachments` | 上传图片/文件附件 |
| GET | `/api/v1/classrooms/:id/attachments/:aid` | 查询附件元数据 |
| GET | `/api/v1/classrooms/:id/attachments/:aid/download` | 下载附件，`?token=` 传 room token |
| POST | `/api/v1/classrooms/:id/recordings/start` | 教师发起录屏，进入待学生确认 |
| POST | `/api/v1/classrooms/:id/recordings/:rid/consent` | 学生同意录屏 |
| POST | `/api/v1/classrooms/:id/recordings/:rid/decline` | 学生拒绝录屏 |
| GET | `/api/v1/classrooms/:id/recordings/:rid` | 查询录屏会话状态 |
| POST | `/api/v1/classrooms/:id/recordings/:rid/stop` | 停止录屏会话（文件已在教师本地保存） |

### 管理后台 API

以下接口需要 `admin` 角色：

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/admin/dashboard` | 运营概览 |
| GET | `/api/v1/admin/users` | 用户列表 |
| POST | `/api/v1/admin/users` | 创建教师/管理员 |
| PUT | `/api/v1/admin/users/:id` | 修改昵称/角色/状态/重置密码 |
| GET | `/api/v1/admin/classrooms` | 课堂列表 |
| GET | `/api/v1/admin/classrooms/:id` | 课堂详情 |
| POST | `/api/v1/admin/classrooms/:id/end` | 管理员结束课堂 |
| GET | `/api/v1/admin/attachments` | 附件列表 |
| DELETE | `/api/v1/admin/attachments/:id` | 删除附件 |
| GET | `/api/v1/admin/audit` | 全局审计日志 |
| GET | `/api/v1/admin/settings` | 读取课堂配额与限流设置 |
| PUT | `/api/v1/admin/settings` | 更新课堂配额与限流设置 |

## WebSocket

浏览器无法给 WebSocket 设置 `Authorization`，room token 通过子协议传递：

```js
const ws = new WebSocket(
  `ws://localhost:18080/api/v1/classrooms/${roomId}/ws`,
  [`ppclass.token.${roomToken}`],
);
```

所有事件统一为 `{ "type": "...", "data": ... }`。

服务端事件：

- `room.state`：连接成功后的房间和参与者快照
- `presence.joined` / `presence.left` / `room.joined` / `room.left`
- `chat.message`：其他人发来的新消息
- `chat.ack`：自己发送消息的确认，含持久化消息和 `created`
- `chat.read` / `typing`
- `screen.state` / `screen.request`
- `peer.publishing` / `peer.unpublished`
- `recording.request` / `recording.consented` / `recording.declined` / `recording.stop.requested` / `recording.state`
- `control.mute` / `control.screen.permission` / `control.chat`
- `room.ended`
- `pong`
- `error`

客户端事件统一用 `{ "type": "...", "data": { ... } }`（服务端同时兼容旧的扁平字段）：

- `chat.send`：`{ "type": "chat.send", "data": { "clientMsgId": "...", "messageType": "text|image|file", "body": "...", "attachmentId": 1 } }`
- `chat.read`：`{ "type": "chat.read", "data": { "upToId": 123 } }`
- `typing.start` / `typing.stop`
- `screen.start.request` / `screen.stop`
- `recording.stop.request`
- `media.ready`：`{ "type": "media.ready", "data": { "leg": "teacher-cam" } }`
- `media.stats`：`{ "type": "media.stats", "data": { "leg": "...", "rttMs": 0, "lossPct": 0, "jitterMs": 0, "bitrateBps": 0 } }`
- `ping` / `room.ping`

## 媒体凭据

`join` 响应包含 `media`；`PPCLASS_PPCDN_ENABLED=false` 时只返回 `{ "enabled": false }`。

```json
{
  "media": {
    "enabled": true,
    "publish": {
      "teacher-cam": {
        "url": "https://origin.example/whip/app/cls1-t",
        "bearerToken": "...",
        "streamPath": "app/cls1-t",
        "expiresAt": "2026-10-31T12:00:00Z"
      }
    },
    "subscribe": {
      "student-cam": {
        "url": "https://edge.example/app/cls1-s?txTime=...&txSecret=...",
        "codec": "h264",
        "streamPath": "app/cls1-s",
        "expiresAt": "2026-10-31T12:00:00Z"
      }
    },
    "iceServers": [],
    "expiresAt": "2026-10-31T12:00:00Z"
  }
}
```

- `publish` 直接把 SDP POST 到 `url`，带 `Authorization: Bearer <bearerToken>`。
- `subscribe.url` 是不含 codec 与 `/whep` 的 edge base，客户端追加 `/h264/whep` 后拉流。
- 对端尚未推流时 `subscribe` 会缺省该腿；收到 `peer.publishing` 后调用 `POST /media/refresh` 续签。
- 学生请求共享屏幕用 WS `screen.start.request`；教师用 `control` 授予 `screen.permission` 后，学生再调用 `screen/start` 获取屏幕推流凭据。
- 若 ppcdn 账号已绑定 ppobs 设备，必须设置 `PPCLASS_PPCDN_DEVICE_ID`；可用 `POST /v1/ppobs-devices/register`（appId/appSecret + 本机 SN/MAC）获取本机 deviceId。
- ppClassVip 请求 `whip-hevc-h264` capability 并优先使用 `whipTracks.h264` 的 codec 路径，这样 Edge 的 `/h264/whep` 才能拉到流；未返回 track 时回退到裸 WHIP 路径。

## 配置项

| 环境变量 | 说明 |
| --- | --- |
| `PPCLASS_APP_NAME` | 应用名，默认 `PPClassVip` |
| `PPCLASS_ENV` | `dev` / `prod` |
| `PPCLASS_HTTP_ADDR` | 监听地址，默认 `:18080` |
| `PPCLASS_HTTP_READ_TIMEOUT` | 读超时，默认 `15s` |
| `PPCLASS_HTTP_WRITE_TIMEOUT` | 写超时，默认 `15s` |
| `PPCLASS_HTTP_SHUTDOWN_TIMEOUT` | 优雅关闭超时，默认 `10s` |
| `PPCLASS_MYSQL_DSN` | MySQL DSN |
| `PPCLASS_MYSQL_MAX_OPEN_CONNS` | 最大连接数，默认 `20` |
| `PPCLASS_MYSQL_MAX_IDLE_CONNS` | 最大空闲连接数，默认 `10` |
| `PPCLASS_MYSQL_CONN_MAX_LIFETIME` | 连接最大存活时间，默认 `30m` |
| `PPCLASS_MYSQL_CONN_MAX_IDLE_TIME` | MySQL 空闲连接时间，默认 `60s` |
| `PPCLASS_MYSQL_AUTO_MIGRATE` | 是否自动迁移表结构，默认 `true` |
| `PPCLASS_JWT_SECRET` | JWT 密钥，生产至少 32 位 |
| `PPCLASS_AUTH_ISSUER` | JWT issuer，默认 `ppclassvip` |
| `PPCLASS_AUTH_ACCESS_TTL` | 教师 access token 有效期，如 `24h` |
| `PPCLASS_CLASSROOM_INVITE_TTL` | 学生邀请有效期，默认 `24h` |
| `PPCLASS_CLASSROOM_ROOM_TOKEN_TTL` | 课堂 room token 有效期，默认 `12h` |
| `PPCLASS_LOG_LEVEL` | `debug` / `info` / `warn` / `error` |
| `PPCLASS_LOG_FORMAT` | `text` / `json` |
| `PPCLASS_HTTP_CORS_ORIGINS` | 逗号分隔的前端源 |
| `PPCLASS_CLASSROOM_CONSENT_TTL` | 学生录屏确认超时，默认 `60s` |
| `PPCLASS_CLASSROOM_IDLE_TTL` | 空闲课堂回收时间，默认 `4h` |
| `PPCLASS_CLASSROOM_MAX_FILE_BYTES` | 单个附件上限，默认 50 MB |
| `PPCLASS_CLASSROOM_MAX_IMAGE_BYTES` | 单张图片上限，默认 10 MB |
| `PPCLASS_CLASSROOM_ROOM_QUOTA_BYTES` | 单课堂附件总量，默认 500 MB |
| `PPCLASS_CLASSROOM_ATTACHMENT_RETENTION` | 附件保留期，默认 365 天 |
| `PPCLASS_CLASSROOM_MESSAGE_RATE_PER_MINUTE` | 每人每分钟消息数 |
| `PPCLASS_CLASSROOM_UPLOAD_RATE_PER_MINUTE` | 每人每分钟上传数 |
| `PPCLASS_CLASSROOM_INVITE_RATE_PER_MINUTE` | 每 IP 每分钟邀请/游客加入数 |
| `PPCLASS_STORAGE_DATA_DIR` | 本地存储根目录，默认 `./data` |
| `PPCLASS_STORAGE_ATTACHMENT_DIR` | 附件目录，默认 `{dataDir}/attachments` |
| `PPCLASS_PPCDN_ENABLED` | 是否启用 PPCDN 媒体凭据 |
| `PPCLASS_PPCDN_BASE_URL` | ppcenter 地址，如 `https://api.pp-cdn.org` |
| `PPCLASS_PPCDN_APP_ID` | 单机构课堂媒体 appId |
| `PPCLASS_PPCDN_APP_SECRET` | 媒体 appSecret |
| `PPCLASS_PPCDN_KEY_ID` | 媒体签名 keyId，默认 `default` |
| `PPCLASS_PPCDN_DEVICE_ID` | 可选；账号开启 ppobs 设备绑定时的发布 deviceId |
| `PPCLASS_PPCDN_REGION` | 可选选点区域 |
| `PPCLASS_PPCDN_TIMEOUT` | 调用 ppcenter 超时，默认 `10s` |
| `PPCLASS_PPCDN_MEDIA_TTL` | 媒体凭据有效期，默认 `10m` |

## 构建

```bash
go build -trimpath -ldflags "-s -w -X ppclassvip/internal/version.Version=0.1.0" -o bin/ppclassvip ./cmd/server
```

## P1 后端范围

当前仓库已完成 P1：课堂生命周期、聊天、附件、屏幕共享凭据、录屏同意与本地录制、PPCDN 媒体凭据、限流、审计和清理，以及前端 `ppclass-web`（React 19 + TypeScript + Vite）。

前端代码在 [`ppclass-web/`](ppclass-web/)，包含教师工作台、学生邀请页、课堂页、`ppclass` SDK 和 `ppscreenrecorder` 录屏模块：

```powershell
cd ppclass-web
npm install
npm run sync:pplayer
npm run dev
```

`ppclass-web` 默认把 `/api` 代理到 `http://127.0.0.1:18080`，并复用 `pplayer` 的 `ppwebpublish.mjs` / `ppplayer.mjs`（通过 `sync:pplayer` 拷贝，不修改外部仓库）。详见 [ppclass-web/README.md](ppclass-web/README.md)。

独立管理后台在 [`ppclass-admin/`](ppclass-admin/)，仅 `admin` 角色可登录，部署在 `/admin/`：包含运营概览、用户管理、课堂详情/结束、附件管理、全局审计和系统设置（配额、限流、保留期、录屏开关、每教师课堂上限），设置保存后立即生效。详见 [ppclass-admin/README.md](ppclass-admin/README.md)。

```powershell
cd ppclass-admin
npm install
npm run dev
```

远程桌面已提供 Windows Agent：教师发起申请，学生同意后启动 Agent，由 Agent 独立采集屏幕并发布 `student-screen`；控制模式下 Agent 通过 `SendInput` 注入鼠标键盘。Agent 支持托盘 UI、本地同意弹窗、开机启动、SHA-256 校验自动更新和断线自动重连。支持只看/控制、单次 30 分钟、禁止与录屏同时进行。详见 [docs/remote-desktop.zh-CN.md](docs/remote-desktop.zh-CN.md)。

Agent 安装包：`https://ppclassvip.pp-cdn.org/downloads/ppclass-agent-setup.exe`。

自动化已验证项与需要人工验收的清单见 [docs/p1-acceptance-status.zh-CN.md](docs/p1-acceptance-status.zh-CN.md)。

当前整体开发进度见 [docs/development-progress.zh-CN.md](docs/development-progress.zh-CN.md)。
