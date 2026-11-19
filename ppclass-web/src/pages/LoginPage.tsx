import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { ApiError, PpclassApi, saveTeacherToken } from '../sdk';

export default function LoginPage() {
  const navigate = useNavigate();
  const [email, setEmail] = useState('teacher@example.com');
  const [password, setPassword] = useState('');
  const [showPassword, setShowPassword] = useState(false);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setLoading(true);
    setError('');
    try {
      const api = new PpclassApi();
      const result = await api.login(email, password);
      saveTeacherToken(result.token);
      navigate('/');
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '登录失败');
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="page narrow">
      <div className="card stack">
        <div>
          <h1 style={{ margin: '0 0 0.3rem' }}>PPClassVip</h1>
          <p className="muted" style={{ margin: 0 }}>
            教师/管理员登录
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
                onClick={() => setShowPassword((visible) => !visible)}
                aria-label={showPassword ? '隐藏密码' : '显示密码'}
                title={showPassword ? '隐藏密码' : '显示密码'}
              >
                {showPassword ? (
                  <svg viewBox="0 0 24 24" aria-hidden="true">
                    <path
                      d="M3 3l18 18M10.6 10.7a2 2 0 002.8 2.8M9.9 5.1A9.8 9.8 0 0112 5c5.5 0 9 5.2 9 7 0 .9-1 2.6-2.8 3.9M6.6 6.6C4.6 7.9 3 9.9 3 12c0 1.8 3.5 7 9 7 1.5 0 2.9-.4 4.1-1.1"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="1.8"
                      strokeLinecap="round"
                      strokeLinejoin="round"
                    />
                  </svg>
                ) : (
                  <svg viewBox="0 0 24 24" aria-hidden="true">
                    <path
                      d="M3 12s3.5-7 9-7 9 7 9 7-3.5 7-9 7-9-7-9-7z"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="1.8"
                      strokeLinejoin="round"
                    />
                    <circle cx="12" cy="12" r="3" fill="none" stroke="currentColor" strokeWidth="1.8" />
                  </svg>
                )}
              </button>
            </div>
          </div>
          {error && <div className="error">{error}</div>}
          <button className="primary" type="submit" disabled={loading}>
            {loading ? '登录中…' : '登录'}
          </button>
        </form>
        <p className="footer-note">首次使用请先创建教师账号。</p>
      </div>
    </div>
  );
}
