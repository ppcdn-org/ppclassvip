import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import type { RoomWithTeacher, Stats } from '../api';
import { ErrorBox, StateBadge, formatBytes, formatDate, useAdminApi } from '../ui';

export default function DashboardPage() {
  const api = useAdminApi();
  const [stats, setStats] = useState<Stats | null>(null);
  const [rooms, setRooms] = useState<RoomWithTeacher[]>([]);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    api
      .dashboard()
      .then((data) => {
        setStats(data.stats);
        setRooms(data.recentRooms || []);
      })
      .catch((err) => setError(err instanceof Error ? err.message : '加载失败'))
      .finally(() => setLoading(false));
  }, [api]);

  const cards = stats
    ? [
        { label: '用户', value: stats.users, hint: `教师 ${stats.teachers} / 管理员 ${stats.admins}` },
        { label: '课堂', value: stats.rooms, hint: `进行中 ${stats.openRooms}` },
        { label: '已结束课堂', value: stats.endedRooms, hint: '历史课堂' },
        { label: '录屏', value: stats.recordings, hint: formatBytes(stats.recordingBytes) },
        { label: '附件', value: stats.attachments, hint: formatBytes(stats.attachmentBytes) },
      ]
    : [];

  return (
    <div className="stack">
      <div className="page-head">
        <div>
          <h1>运营概览</h1>
          <p className="muted">课堂、用户、存储与审计的全局状态。</p>
        </div>
        <Link className="button-link" to="/settings">
          系统设置
        </Link>
      </div>
      <ErrorBox message={error} />
      {loading ? (
        <div className="card muted">加载中…</div>
      ) : (
        <>
          <div className="stat-grid">
            {cards.map((card) => (
              <div className="card stat-card" key={card.label}>
                <div className="muted small">{card.label}</div>
                <div className="stat-value">{card.value}</div>
                <div className="muted small">{card.hint}</div>
              </div>
            ))}
          </div>
          <div className="card">
            <div className="card-head">
              <h2>最近课堂</h2>
              <Link to="/classrooms" className="small">
                查看全部
              </Link>
            </div>
            <div className="table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>ID</th>
                    <th>标题</th>
                    <th>教师</th>
                    <th>状态</th>
                    <th>录屏</th>
                    <th>创建时间</th>
                  </tr>
                </thead>
                <tbody>
                  {rooms.length === 0 ? (
                    <tr>
                      <td colSpan={6} className="empty-cell">
                        暂无课堂
                      </td>
                    </tr>
                  ) : (
                    rooms.map(({ room, teacher }) => (
                      <tr key={room.id}>
                        <td>{room.id}</td>
                        <td>{room.title}</td>
                        <td>{teacher?.nickname || teacher?.email || room.teacherUserId}</td>
                        <td>
                          <StateBadge value={room.state} />
                        </td>
                        <td>{room.recordingState}</td>
                        <td>{formatDate(room.createdAt)}</td>
                      </tr>
                    ))
                  )}
                </tbody>
              </table>
            </div>
          </div>
        </>
      )}
    </div>
  );
}
