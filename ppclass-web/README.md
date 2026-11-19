# ppclass-web

PPClassVip 的 1v1 课堂前端，React 19 + TypeScript + Vite。

## 功能

- 教师：登录、创建课堂、生成一次性邀请链接、进入课堂。
- 学生：邀请页输入昵称、设备检测、进入课堂。
- 课堂：摄像头/麦克风 WHIP 推流、WHEP 拉流、屏幕共享、文字/图片/文件消息、静音/屏幕权限/聊天开关、结束课堂。
- 录屏：教师发起 → 学生同意 → 屏幕 + 教师麦克风 + 学生远端音频混音 → WebM 分片上传 → finalize；支持中断后从录屏记录里完成保存/放弃/下载。
- 弱网：WS 指数退避重连、媒体凭据续签、录屏分片重试。

## 依赖 pplayer

推拉流复用 `pplayer` 的 `ppwebpublish.mjs` 与 `ppplayer.mjs`。外部 pplayer 仓库不修改，构建前把所需模块拷贝到 `public/vendor/pplayer/`：

```bash
npm run sync:pplayer
```

默认从 `../../../../github/ppcdn/pplayer` 读取，也可以用 `PPLAYER_SRC` 指定路径：

```powershell
$env:PPLAYER_SRC='D:\github\ppcdn\pplayer'
npm run sync:pplayer
```

`public/vendor/pplayer/` 已在 `.gitignore` 中忽略。

## 开发

```bash
npm install
npm run sync:pplayer
npm run dev
```

默认地址 `http://127.0.0.1:5173`，`/api` 代理到 `http://127.0.0.1:18080`。后端需要先按仓库 README 启动，并创建教师账号。

如果 API 不在同源，构建时设置：

```bash
VITE_PPCLASS_API_BASE=https://class-api.example.com/api/v1 npm run build
```

## 构建与部署

```bash
npm run build
```

产物在 `dist/`。部署时：

- 静态站点需要 SPA fallback（`/classroom/*`、`/join` 都回退到 `index.html`）。
- 反向代理 `/api/` 到 ppClassVip，并支持 WebSocket 升级。
- 确保站点为 HTTPS：摄像头、屏幕共享、麦克风在现代浏览器中都要求安全上下文。
- `public/vendor/pplayer/` 会随 `dist/` 一起发布。

nginx 片段示例：

```nginx
location / {
    root /var/www/ppclass-web;
    try_files $uri $uri/ /index.html;
}

location /api/ {
    proxy_pass http://127.0.0.1:18080;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_set_header Host $host;
    client_max_body_size 64m;
}
```

## 已知边界

- 教师端优先 Chrome/Edge；Safari 可能不支持 WebM `MediaRecorder`，录屏按钮会禁用。
- 学生默认只能观看/聊天；屏幕共享需要教师授权。
- 录屏在教师浏览器完成，服务端只接收分片和 finalize，不做服务端转封装。
- 本地未配置 PPCDN 时，课堂聊天/文件可用，但音视频/屏幕共享/录屏不可用。
