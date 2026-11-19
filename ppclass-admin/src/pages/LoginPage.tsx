import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { AdminApi, ApiError } from '../api';
import { saveAdminToken } from '../auth';

export default function LoginPage() {
  const navigate = useNavigate();
  const [email, setEmail] = useState('admin@example.com');
  const [password, setPassword] = useState('');
  const [showPassword, setShowPassword] = useState(false);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setLoading(true);
    setError('');
    try {
      const api = new AdminApi();
      const result = await api.login(email, password);
      saveAdminToken(result.token);
      localStorage.setItem('ppclass.adminEmail', result.user.email);
      navigate('/');
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '登录失败');
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="login-wrap">
      <div className="card stack login-card">
        <div>
          <div className="brand large">
            <span className="brand-mark">PV</span>
            <div>
              <strong>PPClassVip</strong>
              <div className="brand-sub">管理后台</div>
            </div>
          </div>
          <p className="muted" style={{ marginBottom: 0 }}>
            仅限管理员账号登录
          </p>
        </div>
        <form className="stack" onSubmit={submit}>
          <div>
            <label htmlFor="email">邮箱</label>
            <input
              id="email"
              type="email"
              autoComplete="username"
              value={email}
              onChange={(event) => setEmail(event.target.value)}
              required
            />
          </div>
          <div>
            <label htmlFor="password">密码</label>
            <div className="password-field">
              <input
                id="password"
                type={showPassword ? 'text' : 'password'}
                autoComplete="current-password"
                value={password}
                onChange={(event) => setPassword(event.target.value)}
                required
              />
              <button
                type="button"
                className="password-toggle"
                onClick={() => setShowPassword((value) => !value)}
                aria-label={showPassword ? '隐藏密码' : '显示密码'}
              >
                {showPassword ? '隐藏' : '显示'}
              </button>
            </div>
          </div>
          {error && <div className="error">{error}</div>}
          <button className="primary" type="submit" disabled={loading}>
            {loading ? '登录中…' : '登录管理后台'}
          </button>
        </form>
      </div>
    </div>
  );
}
