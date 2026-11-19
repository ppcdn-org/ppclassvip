import { useCallback, useEffect, useState } from 'react';
import type { AuditEntry } from '../api';
import {
  ErrorBox,
  Pagination,
  TableEmpty,
  formatDate,
  useAdminApi,
} from '../ui';

export default function AuditPage() {
  const api = useAdminApi();
  const [items, setItems] = useState<AuditEntry[]>([]);
  const [total, setTotal] = useState(0);
  const [offset, setOffset] = useState(0);
  const [roomId, setRoomId] = useState('');
  const [action, setAction] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const limit = 50;

  const load = useCallback(async (nextOffset = offset) => {
    setError('');
    setLoading(true);
    try {
      const result = await api.listAudit({
        roomId: roomId ? Number(roomId) : undefined,
        action,
        limit,
        offset: nextOffset,
      });
      setItems(result.entries);
      setTotal(result.total);
      setOffset(result.offset);
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载审计失败');
    } finally {
      setLoading(false);
    }
  }, [api, limit, offset, roomId, action]);

  useEffect(() => {
    void load(0);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [roomId, action]);

  return (
    <div className="stack">
      <div className="page-head">
        <div>
          <h1>审计日志</h1>
          <p className="muted">课堂与管理操作审计，不记录消息正文。</p>
        </div>
      </div>
      <ErrorBox message={error} />
      <div className="card">
        <div className="filters">
          <input
            placeholder="课堂 ID"
            value={roomId}
            onChange={(event) => setRoomId(event.target.value.replace(/\D/g, ''))}
          />
          <input
            placeholder="动作关键字，如 room.end"
            value={action}
            onChange={(event) => setAction(event.target.value)}
          />
          <button onClick={() => void load(0)}>刷新</button>
        </div>
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>ID</th>
                <th>时间</th>
                <th>课堂</th>
                <th>用户</th>
                <th>参与者</th>
                <th>动作</th>
                <th>详情</th>
                <th>IP</th>
              </tr>
            </thead>
            <tbody>
              {loading ? (
                <tr>
                  <td colSpan={8} className="empty-cell">
                    加载中…
                  </td>
                </tr>
              ) : items.length === 0 ? (
                <TableEmpty colSpan={8} />
              ) : (
                items.map((item) => (
                  <tr key={item.id}>
                    <td>{item.id}</td>
                    <td>{formatDate(item.createdAt)}</td>
                    <td>{item.roomId || '-'}</td>
                    <td>{item.userId || '-'}</td>
                    <td>{item.participantId || '-'}</td>
                    <td className="mono">{item.action}</td>
                    <td className="detail-cell">{item.detail}</td>
                    <td>{item.clientIp}</td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
        <Pagination total={total} limit={limit} offset={offset} onChange={(value) => void load(value)} />
      </div>
    </div>
  );
}
