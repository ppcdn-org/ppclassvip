# ppclass-admin

PPClassVip 独立管理后台，仅 `admin` 角色可登录。部署在 `/admin/`，与 `ppclass-web` 同源但独立构建。

## 功能

- 管理员登录（密码可见切换），非管理员账号拒绝登录。
- 运营概览：用户、教师、管理员、课堂、录屏、附件、存储占用。
- 用户管理：创建教师/管理员、修改昵称/角色/状态、重置密码。
- 课堂管理：筛选、详情（参与者/录屏/附件/审计）、管理员结束课堂。
- 录屏管理：列表、下载、删除（物理删除文件并标记 purged）。
- 附件管理：列表、下载、删除。
- 审计日志：按课堂、动作筛选。
- 系统设置：文件/图片/课堂配额、分片/录屏上限、录屏开关、确认/空闲 TTL、保留期、限流、每教师课堂上限；保存后立即生效。

## 开发

```bash
npm install
npm run dev
```

默认地址 `http://127.0.0.1:5174/admin/`，`/api` 代理到 `http://127.0.0.1:18080`。

## 构建

```bash
npm run build
```

产物在 `dist/`，部署到 `/var/www/ppclass-admin/`，nginx：

```nginx
location /admin/ {
    alias /var/www/ppclass-admin/;
    try_files $uri $uri/ /admin/index.html;
}
```

管理后台依赖后端 `/api/v1/admin/*` 接口和 `admin` 角色账号。
