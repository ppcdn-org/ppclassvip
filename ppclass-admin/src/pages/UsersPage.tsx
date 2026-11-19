import { useCallback, useEffect, useState } from 'react';
import type { User } from '../api';
import {
  ErrorBox,
  Pagination,
  StateBadge,
  TableEmpty,
  formatDate,
  useAdminApi,
} from '../ui';

const emptyCreate = { email: '', nickname: '', password: '', role: 'teacher' };

export default function UsersPage() {
  const api = useAdminApi();
  const [users, setUsers] = useState<User[]>([]);
  const [total, setTotal] = useState(0);
  const [offset, setOffset] = useState(0);
  const [query, setQuery] = useState('');
  const [role, setRole] = useState('');
  const [status, setStatus] = useState('');
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [loading, setLoading] = useState(true);
  const [createOpen, setCreateOpen] = useState(false);
  const [createForm, setCreateForm] = useState(emptyCreate);
  const [editing, setEditing] = useState<User | null>(null);

  const limit = 50;

  const load = useCallback(async (nextOffset = offset) => {
    setError('');
    setLoading(true);
    try {
      const result = await api.listUsers({ query, role, status, limit, offset: nextOffset });
      setUsers(result.users);
      setTotal(result.total);
      setOffset(result.offset);
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载用户失败');
    } finally {
      setLoading(false);
    }
  }, [api, limit, offset, query, role, status]);

  useEffect(() => {
    void load(0);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query, role, status]);

  async function createUser(event: React.FormEvent) {
    event.preventDefault();
    setError('');
    setNotice('');
    try {
      await api.createUser(createForm);
      setCreateForm(emptyCreate);
      setCreateOpen(false);
      setNotice('用户已创建');
      await load(0);
    } catch (err) {
      setError(err instanceof Error ? err.message : '创建用户失败');
    }
  }

  async function toggleStatus(user: User) {
    const next = user.status === 'active' ? 'disabled' : 'active';
    if (!window.confirm(`确定将 ${user.email} 设为 ${next} 吗？`)) return;
    setError('');
    try {
      await api.updateUser(user.id, { status: next });
      setNotice('用户状态已更新');
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : '更新用户失败');
    }
  }

  return (
    <div className="stack">
      <div className="page-head">
        <div>
          <h1>用户管理</h1>
          <p className="muted">教师与管理员账号，支持创建、禁用、改角色和重置密码。</p>
        </div>
        <button className="primary" onClick={() => setCreateOpen((value) => !value)}>
          {createOpen ? '收起创建表单' : '创建用户'}
        </button>
      </div>
      <ErrorBox message={error} />
      {notice && <div className="notice">{notice}</div>}

      {createOpen && (
        <form className="card form-grid" onSubmit={createUser}>
          <div>
            <label>邮箱</label>
            <input
              type="email"
              value={createForm.email}
              onChange={(event) => setCreateForm({ ...createForm, email: event.target.value })}
              required
            />
          </div>
          <div>
            <label>昵称</label>
            <input
              value={createForm.nickname}
              onChange={(event) => setCreateForm({ ...createForm, nickname: event.target.value })}
              placeholder="可留空"
            />
          </div>
          <div>
            <label>初始密码</label>
            <input
              type="password"
              value={createForm.password}
              onChange={(event) => setCreateForm({ ...createForm, password: event.target.value })}
              required
            />
          </div>
          <div>
            <label>角色</label>
            <select
              value={createForm.role}
              onChange={(event) => setCreateForm({ ...createForm, role: event.target.value })}
            >
              <option value="teacher">教师</option>
              <option value="admin">管理员</option>
            </select>
          </div>
          <div className="form-actions">
            <button className="primary" type="submit">
              创建
            </button>
          </div>
        </form>
      )}

      <div className="card">
        <div className="filters">
          <input
            placeholder="搜索邮箱/昵称"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
          <select value={role} onChange={(event) => setRole(event.target.value)}>
            <option value="">全部角色</option>
            <option value="teacher">教师</option>
            <option value="admin">管理员</option>
          </select>
          <select value={status} onChange={(event) => setStatus(event.target.value)}>
            <option value="">全部状态</option>
            <option value="active">active</option>
            <option value="disabled">disabled</option>
          </select>
          <button onClick={() => void load(0)}>刷新</button>
        </div>
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>ID</th>
                <th>邮箱</th>
                <th>昵称</th>
                <th>角色</th>
                <th>状态</th>
                <th>创建时间</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {loading ? (
                <tr>
                  <td colSpan={7} className="empty-cell">
                    加载中…
                  </td>
                </tr>
              ) : users.length === 0 ? (
                <TableEmpty colSpan={7} />
              ) : (
                users.map((user) => (
                  <tr key={user.id}>
                    <td>{user.id}</td>
                    <td>{user.email}</td>
                    <td>{user.nickname}</td>
                    <td>
                      <StateBadge value={user.role} />
                    </td>
                    <td>
                      <StateBadge value={user.status} />
                    </td>
                    <td>{formatDate(user.createdAt)}</td>
                    <td className="actions">
                      <button className="small" onClick={() => setEditing(user)}>
                        编辑
                      </button>
                      <button className="small" onClick={() => void toggleStatus(user)}>
                        {user.status === 'active' ? '禁用' : '启用'}
                      </button>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
        <Pagination total={total} limit={limit} offset={offset} onChange={(value) => void load(value)} />
      </div>

      {editing && (
        <EditUserModal
          user={editing}
          onClose={() => setEditing(null)}
          onSaved={async () => {
            setEditing(null);
            setNotice('用户已更新');
            await load();
          }}
        />
      )}
    </div>
  );
}

function EditUserModal({
  user,
  onClose,
  onSaved,
}: {
  user: User;
  onClose: () => void;
  onSaved: () => Promise<void>;
}) {
  const api = useAdminApi();
  const [nickname, setNickname] = useState(user.nickname);
  const [role, setRole] = useState(user.role);
  const [status, setStatus] = useState(user.status);
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setSaving(true);
    setError('');
    try {
      await api.updateUser(user.id, {
        nickname,
        role,
        status,
        ...(password ? { password } : {}),
      });
      await onSaved();
    } catch (err) {
      setError(err instanceof Error ? err.message : '更新失败');
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="modal-backdrop">
      <form className="modal stack" onSubmit={submit}>
        <div className="card-head">
          <h2>编辑用户</h2>
          <button type="button" className="ghost" onClick={onClose}>
            关闭
          </button>
        </div>
        <div className="muted small">{user.email}</div>
        <div>
          <label>昵称</label>
          <input value={nickname} onChange={(event) => setNickname(event.target.value)} required />
        </div>
        <div className="form-grid">
          <div>
            <label>角色</label>
            <select value={role} onChange={(event) => setRole(event.target.value as typeof role)}>
              <option value="teacher">教师</option>
              <option value="admin">管理员</option>
            </select>
          </div>
          <div>
            <label>状态</label>
            <select value={status} onChange={(event) => setStatus(event.target.value as typeof status)}>
              <option value="active">active</option>
              <option value="disabled">disabled</option>
            </select>
          </div>
        </div>
        <div>
          <label>重置密码（留空则不变）</label>
          <input
            type="password"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            placeholder="至少 8 位"
          />
        </div>
        <ErrorBox message={error} />
        <div className="form-actions">
          <button type="button" onClick={onClose}>
            取消
          </button>
          <button className="primary" type="submit" disabled={saving}>
            {saving ? '保存中…' : '保存'}
          </button>
        </div>
      </form>
    </div>
  );
}
