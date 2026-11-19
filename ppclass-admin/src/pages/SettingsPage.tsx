import { useEffect, useState } from 'react';
import type { Settings } from '../api';
import { ErrorBox, formatDate, useAdminApi } from '../ui';

const MB = 1024 * 1024;

export default function SettingsPage() {
  const api = useAdminApi();
  const [settings, setSettings] = useState<Settings | null>(null);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    api
      .getSettings()
      .then((data) => setSettings(data.settings))
      .catch((err) => setError(err instanceof Error ? err.message : '加载设置失败'))
      .finally(() => setLoading(false));
  }, [api]);

  async function save(event: React.FormEvent) {
    event.preventDefault();
    if (!settings) return;
    setSaving(true);
    setError('');
    setNotice('');
    try {
      const result = await api.updateSettings(settings);
      setSettings(result.settings);
      setNotice('设置已保存并立即生效');
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存失败');
    } finally {
      setSaving(false);
    }
  }

  if (loading || !settings) {
    return <div className="card muted">加载中…</div>;
  }

  return (
    <div className="stack">
      <div className="page-head">
        <div>
          <h1>系统设置</h1>
          <p className="muted">修改后立即生效，无需重启服务。</p>
        </div>
      </div>
      <ErrorBox message={error} />
      {notice && <div className="notice">{notice}</div>}
      <form className="card stack" onSubmit={save}>
        <h2>存储与录屏</h2>
        <div className="form-grid">
          <NumberField
            label="单个文件上限 (MB)"
            value={settings.maxFileBytes / MB}
            onChange={(value) => setSettings({ ...settings, maxFileBytes: Math.round(value * MB) })}
          />
          <NumberField
            label="单张图片上限 (MB)"
            value={settings.maxImageBytes / MB}
            onChange={(value) => setSettings({ ...settings, maxImageBytes: Math.round(value * MB) })}
          />
          <NumberField
            label="单课堂附件总量 (MB)"
            value={settings.roomQuotaBytes / MB}
            onChange={(value) => setSettings({ ...settings, roomQuotaBytes: Math.round(value * MB) })}
          />
          <div>
            <label>录屏开关</label>
            <label className="checkbox">
              <input
                type="checkbox"
                checked={settings.recordingEnabled}
                onChange={(event) => setSettings({ ...settings, recordingEnabled: event.target.checked })}
              />
              允许教师发起录屏（录屏保存在教师本机）
            </label>
          </div>
        </div>

        <h2>课堂与保留期</h2>
        <div className="form-grid">
          <NumberField
            label="学生录屏确认超时 (秒)"
            value={settings.consentTtlSeconds}
            onChange={(value) => setSettings({ ...settings, consentTtlSeconds: Math.round(value) })}
          />
          <NumberField
            label="空闲课堂回收 (秒)"
            value={settings.idleTtlSeconds}
            onChange={(value) => setSettings({ ...settings, idleTtlSeconds: Math.round(value) })}
          />
          <NumberField
            label="附件保留期 (天)"
            value={settings.attachmentRetentionDays}
            onChange={(value) => setSettings({ ...settings, attachmentRetentionDays: Math.round(value) })}
          />
          <NumberField
            label="每教师并发课堂上限"
            value={settings.maxRoomsPerTeacher}
            onChange={(value) => setSettings({ ...settings, maxRoomsPerTeacher: Math.round(value) })}
          />
        </div>

        <h2>限流（每分钟）</h2>
        <div className="form-grid">
          <NumberField
            label="每人消息数"
            value={settings.messageRatePerMinute}
            onChange={(value) => setSettings({ ...settings, messageRatePerMinute: Math.round(value) })}
          />
          <NumberField
            label="每人上传数"
            value={settings.uploadRatePerMinute}
            onChange={(value) => setSettings({ ...settings, uploadRatePerMinute: Math.round(value) })}
          />
          <NumberField
            label="每 IP 邀请/游客加入数"
            value={settings.inviteRatePerMinute}
            onChange={(value) => setSettings({ ...settings, inviteRatePerMinute: Math.round(value) })}
          />
        </div>

        <div className="form-actions">
          <span className="muted small">上次更新：{formatDate(settings.updatedAt)}</span>
          <button className="primary" type="submit" disabled={saving}>
            {saving ? '保存中…' : '保存设置'}
          </button>
        </div>
      </form>
    </div>
  );
}

function NumberField({
  label,
  value,
  onChange,
}: {
  label: string;
  value: number;
  onChange: (value: number) => void;
}) {
  return (
    <div>
      <label>{label}</label>
      <input
        type="number"
        min={1}
        step={1}
        value={Number.isFinite(value) ? value : 0}
        onChange={(event) => onChange(Number(event.target.value))}
      />
    </div>
  );
}
