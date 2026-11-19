import { useMemo } from 'react';
import { AdminApi } from './api';
import { loadAdminToken } from './auth';

export function useAdminApi() {
  return useMemo(() => {
    const api = new AdminApi();
    api.setToken(loadAdminToken());
    return api;
  }, []);
}

export function formatBytes(value: number) {
  if (!value) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let index = 0;
  let size = value;
  while (size >= 1024 && index < units.length - 1) {
    size /= 1024;
    index += 1;
  }
  return `${size.toFixed(index === 0 ? 0 : 1)} ${units[index]}`;
}

export function formatDate(value?: string) {
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

export function ErrorBox({ message }: { message: string }) {
  if (!message) return null;
  return <div className="error">{message}</div>;
}

export function StateBadge({ value }: { value: string }) {
  const className =
    value === 'active' || value === 'completed' || value === 'stored'
      ? 'badge ok'
      : value === 'waiting' || value === 'pending_consent' || value === 'recording'
        ? 'badge warn'
        : value === 'ended' || value === 'failed' || value === 'disabled' || value === 'declined'
          ? 'badge danger'
          : 'badge';
  return <span className={className}>{value}</span>;
}

export function Pagination({
  total,
  limit,
  offset,
  onChange,
}: {
  total: number;
  limit: number;
  offset: number;
  onChange: (offset: number) => void;
}) {
  const page = Math.floor(offset / limit) + 1;
  const pages = Math.max(1, Math.ceil(total / limit));
  return (
    <div className="pagination">
      <span className="muted small">
        共 {total} 条 · 第 {page}/{pages} 页
      </span>
      <div className="row">
        <button disabled={offset <= 0} onClick={() => onChange(Math.max(0, offset - limit))}>
          上一页
        </button>
        <button disabled={offset + limit >= total} onClick={() => onChange(offset + limit)}>
          下一页
        </button>
      </div>
    </div>
  );
}

export function TableEmpty({ colSpan, text = '暂无数据' }: { colSpan: number; text?: string }) {
  return (
    <tr>
      <td colSpan={colSpan} className="empty-cell">
        {text}
      </td>
    </tr>
  );
}
