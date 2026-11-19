import { useCallback, useEffect, useState } from 'react';
import type { Attachment } from '../api';
import {
  ErrorBox,
  Pagination,
  StateBadge,
  TableEmpty,
  formatBytes,
  formatDate,
  useAdminApi,
} from '../ui';

export default function AttachmentsPage() {
  const api = useAdminApi();
  const [items, setItems] = useState<Attachment[]>([]);
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
      const result = await api.listAttachments({
        roomId: roomId ? Number(roomId) : undefined,
        status,
        limit,
        offset: nextOffset,
      });
      setItems(result.attachments);
      setTotal(result.total);
      setOffset(result.offset);
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载附件失败');
    } finally {
      setLoading(false);
    }
  }, [api, limit, offset, roomId, status]);

  useEffect(() => {
    void load(0);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [roomId, status]);

  async function remove(item: Attachment) {
    if (!window.confirm(`确定删除附件 ${item.originalName}？文件会被物理删除。`)) return;
    setError('');
    try {
      await api.deleteAttachment(item.id);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : '删除失败');
    }
  }

  return (
    <div className="stack">
      <div className="page-head">
        <div>
          <h1>附件管理</h1>
          <p className="muted">查看和删除课堂附件。</p>
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
            {['stored', 'purged', 'quarantined'].map((item) => (
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
                <th>文件名</th>
                <th>类型</th>
                <th>大小</th>
                <th>状态</th>
                <th>上传时间</th>
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
                    <td>{item.originalName}</td>
                    <td>{item.contentType || item.kind}</td>
                    <td>{formatBytes(item.size)}</td>
                    <td>
                      <StateBadge value={item.status} />
                    </td>
                    <td>{formatDate(item.createdAt)}</td>
                    <td className="actions">
                      {item.status === 'stored' && (
                        <a
                          className="button-link small"
                          href={api.attachmentUrl(item.roomId, item.id)}
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
