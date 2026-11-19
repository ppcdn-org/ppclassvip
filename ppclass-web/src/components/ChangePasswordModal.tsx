import { useState } from 'react';
import { ApiError, type PpclassApi } from '../sdk';

export default function ChangePasswordModal({
  api,
  onClose,
  onChanged,
}: {
  api: PpclassApi;
  onClose: () => void;
  onChanged?: () => void;
}) {
  const [currentPassword, setCurrentPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setError('');
    if (newPassword !== confirmPassword) {
      setError('两次输入的新密码不一致');
      return;
    }
    setSaving(true);
    try {
      await api.changePassword(currentPassword, newPassword);
      onChanged?.();
      onClose();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '修改密码失败');
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="modal-backdrop">
      <form className="modal stack" onSubmit={submit}>
        <div className="row between">
          <h2 style={{ margin: 0 }}>修改密码</h2>
          <button type="button" className="ghost" onClick={onClose}>
            关闭
          </button>
        </div>
        <div>
          <label>当前密码</label>
          <input
            type="password"
            autoComplete="current-password"
            value={currentPassword}
            onChange={(event) => setCurrentPassword(event.target.value)}
            required
          />
        </div>
        <div>
          <label>新密码（至少 8 位）</label>
          <input
            type="password"
            autoComplete="new-password"
            value={newPassword}
            onChange={(event) => setNewPassword(event.target.value)}
            required
          />
        </div>
        <div>
          <label>确认新密码</label>
          <input
            type="password"
            autoComplete="new-password"
            value={confirmPassword}
            onChange={(event) => setConfirmPassword(event.target.value)}
            required
          />
        </div>
        {error && <div className="error">{error}</div>}
        <div className="row" style={{ justifyContent: 'flex-end' }}>
          <button type="button" onClick={onClose}>
            取消
          </button>
          <button className="primary" type="submit" disabled={saving}>
            {saving ? '保存中…' : '保存新密码'}
          </button>
        </div>
      </form>
    </div>
  );
}
