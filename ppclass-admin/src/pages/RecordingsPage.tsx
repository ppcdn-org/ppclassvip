import { useCallback, useEffect, useState } from 'react';
import type { Recording } from '../api';
import {
  ErrorBox,
  Pagination,
  StateBadge,
  TableEmpty,
  formatBytes,
  formatDate,
  useAdminApi,
} from '../ui';

export default function RecordingsPage() {
  const api = useAdminApi();
  const [items, setItems] = useState<Recording[]>([]);
  const [total, setTotal] = useState(0);
  const [offset, setOffset] = useState(0);
  const [roomId, setRoomId] = useState('');
  const [status, setStatus] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const limit = 50;

  const load = useCallback(async (nextOffset = offset) => {
    setError('');
    setLoading(true);
    try {
      const result = await api.listRecordings({
        roomId: roomId ? Number(roomId) : undefined,
        status,
        limit,
        offset: nextOffset,
      });
      setItems(result.recordings);
      setTotal(result.total);
      setOffset(result.offset);
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载录屏失败');
    } finally {
      setLoading(false);
    }
  }, [api, limit, offset, roomId, status]);

  useEffect(() => {
    void load(0);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [roomId, status]);

  async function remove(item: Recording) {
    if (!window.confirm(`确定删除录屏 ${item.recordingId}？文件会被物理删除。`)) return;
    setError('');
    try {
      await api.deleteRecording(item.id);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : '删除失败');
    }
  }

  return (
    <div className="stack">
      <div className="page-head">
        <div>
          <h1>录屏管理</h1>
          <p className="muted">查看和删除课堂录屏。</p>
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
          <select value={status} onChange={(event) => setStatus(event.target.value)}>
            <option value="">全部状态</option>
            {['pending_consent', 'recording', 'completed', 'interrupted', 'failed', 'declined', 'purged'].map((item) => (
              <option key={item} value={item}>
                {item}
              </option>
            ))}
          </select>
          <button onClick={() => void load(0)}>刷新</button>
        </div>
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>ID</th>
                <th>课堂</th>
                <th>录制 ID</th>
                <th>状态</th>
                <th>大小</th>
                <th>分片</th>
                <th>开始时间</th>
                <th>操作</th>
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
                    <td>{item.roomId}</td>
                    <td className="mono">{item.recordingId}</td>
                    <td>
                      <StateBadge value={item.status} />
                    </td>
                    <td>{formatBytes(item.sizeBytes)}</td>
                    <td>{item.lastChunkSeq + 1}</td>
                    <td>{formatDate(item.startedAt)}</td>
                    <td className="actions">
                      {['completed', 'interrupted'].includes(item.status) && (
                        <a
                          className="button-link small"
                          href={api.recordingUrl(item.roomId, item.recordingId)}
                          target="_blank"
                          rel="noreferrer"
                        >
                          下载
                        </a>
                      )}
                      {item.status !== 'purged' && (
                        <button className="small danger" onClick={() => void remove(item)}>
                          删除
                        </button>
                      )}
                    </td>
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
