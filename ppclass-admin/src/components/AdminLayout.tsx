import { useEffect, useMemo, useState } from 'react';
import { NavLink, Outlet, useNavigate } from 'react-router-dom';
import { AdminApi, ApiError } from '../api';
import ChangePasswordModal from './ChangePasswordModal';
import { clearAdminToken, loadAdminToken } from '../auth';

const links = [
  { to: '/', label: '概览', end: true },
  { to: '/users', label: '用户' },
  { to: '/classrooms', label: '课堂' },
  { to: '/attachments', label: '附件' },
  { to: '/audit', label: '审计' },
  { to: '/settings', label: '系统设置' },
];

export default function AdminLayout() {
  const navigate = useNavigate();
  const [email, setEmail] = useState(localStorage.getItem('ppclass.adminEmail') || '');
  const [checking, setChecking] = useState(true);
  const [passwordOpen, setPasswordOpen] = useState(false);
  const [notice, setNotice] = useState('');
  const apiRef = useMemo(() => {
    const api = new AdminApi();
    api.setToken(loadAdminToken());
    return api;
  }, []);

  useEffect(() => {
    const token = loadAdminToken();
    if (!token) {
      navigate('/login', { replace: true });
      return;
    }
    const api = new AdminApi();
    api.setToken(token);
    api
      .me()
      .then((user) => {
        if (user.role !== 'admin') throw new ApiError(403, 'admin_required', 'not admin');
        setEmail(user.email);
        localStorage.setItem('ppclass.adminEmail', user.email);
      })
      .catch(() => {
        clearAdminToken();
        navigate('/login', { replace: true });
      })
      .finally(() => setChecking(false));
  }, [navigate]);

  function logout() {
    clearAdminToken();
    localStorage.removeItem('ppclass.adminEmail');
    navigate('/login');
  }

  if (checking) {
    return <div className="page-loader">正在验证管理员身份…</div>;
  }

  return (
    <div className="admin-shell">
      <aside className="admin-sidebar">
        <div className="brand">
          <span className="brand-mark">PV</span>
          <div>
            <strong>PPClassVip</strong>
            <div className="brand-sub">管理后台</div>
          </div>
        </div>
        <nav>
          {links.map((link) => (
            <NavLink
              key={link.to}
              to={link.to}
              end={link.end}
              className={({ isActive }) => (isActive ? 'nav-link active' : 'nav-link')}
            >
              {link.label}
            </NavLink>
          ))}
        </nav>
        <div className="sidebar-footer">
          <div className="muted small">{email || '管理员'}</div>
          {notice && <div className="notice small">{notice}</div>}
          <div className="row">
            <button className="ghost small" onClick={() => setPasswordOpen(true)}>
              修改密码
            </button>
            <button className="ghost small" onClick={logout}>
              退出登录
            </button>
          </div>
        </div>
      </aside>
      <main className="admin-main">
        <Outlet />
      </main>
      {passwordOpen && (
        <ChangePasswordModal
          api={apiRef}
          onClose={() => setPasswordOpen(false)}
          onChanged={() => setNotice('密码已修改')}
        />
      )}
    </div>
  );
}

