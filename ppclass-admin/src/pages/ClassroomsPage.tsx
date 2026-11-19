import { useCallback, useEffect, useState } from 'react';
import {
  type Attachment,
  type AuditEntry,
  type Participant,
  type RoomWithTeacher,
  type TeacherOption,
} from '../api';
import {
  ErrorBox,
  Pagination,
  StateBadge,
  TableEmpty,
  formatBytes,
  formatDate,
  useAdminApi,
} from '../ui';

export default function ClassroomsPage() {
  const api = useAdminApi();
  const [rooms, setRooms] = useState<RoomWithTeacher[]>([]);
  const [teachers, setTeachers] = useState<TeacherOption[]>([]);
  const [total, setTotal] = useState(0);
  const [offset, setOffset] = useState(0);
  const [query, setQuery] = useState('');
  const [state, setState] = useState('');
  const [teacherId, setTeacherId] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const [detailId, setDetailId] = useState<number | null>(null);
  const limit = 50;

  const load = useCallback(async (nextOffset = offset) => {
    setError('');
    setLoading(true);
    try {
      const result = await api.listClassrooms({
        query,
        state,
        teacherId: teacherId ? Number(teacherId) : undefined,
        limit,
        offset: nextOffset,
      });
      setRooms(result.rooms);
      setTeachers(result.teachers || []);
      setTotal(result.total);
      setOffset(result.offset);
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载课堂失败');
    } finally {
      setLoading(false);
    }
  }, [api, limit, offset, query, state, teacherId]);

  useEffect(() => {
    void load(0);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query, state, teacherId]);

  async function endRoom(id: number) {
    if (!window.confirm('确定结束这个课堂吗？')) return;
    setError('');
    try {
      await api.endClassroom(id);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : '结束课堂失败');
    }
  }

  return (
    <div className="stack">
      <div className="page-head">
        <div>
          <h1>课堂管理</h1>
          <p className="muted">查看全部课堂、参与者、录屏、附件与审计。</p>
        </div>
      </div>
      <ErrorBox message={error} />
      <div className="card">
        <div className="filters">
          <input
            placeholder="搜索标题/房间号"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
          <select value={state} onChange={(event) => setState(event.target.value)}>
            <option value="">全部状态</option>
            {['created', 'waiting', 'active', 'ended', 'failed', 'expired'].map((item) => (
              <option key={item} value={item}>
                {item}
              </option>
            ))}
          </select>
          <select value={teacherId} onChange={(event) => setTeacherId(event.target.value)}>
            <option value="">全部教师</option>
            {teachers.map((teacher) => (
              <option key={teacher.id} value={teacher.id}>
                {teacher.nickname || teacher.email}
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
                <th>标题</th>
                <th>教师</th>
                <th>状态</th>
                <th>录屏</th>
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
              ) : rooms.length === 0 ? (
                <TableEmpty colSpan={7} />
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
                    <td className="actions">
                      <button className="small" onClick={() => setDetailId(room.id)}>
                        详情
                      </button>
                      {!['ended', 'failed', 'expired'].includes(room.state) && (
                        <button className="small danger" onClick={() => void endRoom(room.id)}>
                          结束
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
      {detailId != null && (
        <ClassroomDetailModal id={detailId} onClose={() => setDetailId(null)} onEnded={() => void load()} />
      )}
    </div>
  );
}

function ClassroomDetailModal({
  id,
  onClose,
  onEnded,
}: {
  id: number;
  onClose: () => void;
  onEnded: () => void;
}) {
  const api = useAdminApi();
  const [data, setData] = useState<{
    room: { id: number; title: string; state: string; recordingState: string; roomCode: string; createdAt: string };
    teacher?: { email: string; nickname: string };
    participants: Participant[];
    attachments: Attachment[];
    audit: AuditEntry[];
  } | null>(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    api
      .getClassroom(id)
      .then((result) => setData(result))
      .catch((err) => setError(err instanceof Error ? err.message : '加载详情失败'))
      .finally(() => setLoading(false));
  }, [api, id]);

  async function endRoom() {
    if (!window.confirm('确定结束这个课堂吗？')) return;
    try {
      await api.endClassroom(id);
      onEnded();
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : '结束课堂失败');
    }
  }

  return (
    <div className="modal-backdrop">
      <div className="modal wide stack">
        <div className="card-head">
          <h2>课堂详情 #{id}</h2>
          <button className="ghost" onClick={onClose}>
            关闭
          </button>
        </div>
        <ErrorBox message={error} />
        {loading || !data ? (
          <div className="muted">加载中…</div>
        ) : (
          <>
            <div className="detail-grid">
              <div>
                <span className="muted small">标题</span>
                <div>{data.room.title}</div>
              </div>
              <div>
                <span className="muted small">房间号</span>
                <div>{data.room.roomCode}</div>
              </div>
              <div>
                <span className="muted small">教师</span>
                <div>{data.teacher?.nickname || data.teacher?.email || '-'}</div>
              </div>
              <div>
                <span className="muted small">状态</span>
                <div>
                  <StateBadge value={data.room.state} /> {data.room.recordingState}
                </div>
              </div>
            </div>

            <h3>参与者</h3>
            <div className="table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>ID</th>
                    <th>昵称</th>
                    <th>角色</th>
                    <th>游客</th>
                    <th>加入时间</th>
                    <th>离开时间</th>
                  </tr>
                </thead>
                <tbody>
                  {data.participants.length === 0 ? (
                    <TableEmpty colSpan={6} />
                  ) : (
                    data.participants.map((item) => (
                      <tr key={item.id}>
                        <td>{item.id}</td>
                        <td>{item.displayName}</td>
                        <td>{item.role}</td>
                        <td>{item.guest ? '是' : '否'}</td>
                        <td>{formatDate(item.joinedAt)}</td>
                        <td>{formatDate(item.leftAt)}</td>
                      </tr>
                    ))
                  )}
                </tbody>
              </table>
            </div>

            <h3>附件</h3>
            <div className="table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>ID</th>
                    <th>文件名</th>
                    <th>类型</th>
                    <th>大小</th>
                    <th>状态</th>
                  </tr>
                </thead>
                <tbody>
                  {data.attachments.length === 0 ? (
                    <TableEmpty colSpan={5} />
                  ) : (
                    data.attachments.map((item) => (
                      <tr key={item.id}>
                        <td>{item.id}</td>
                        <td>{item.originalName}</td>
                        <td>{item.contentType}</td>
                        <td>{formatBytes(item.size)}</td>
                        <td>
                          <StateBadge value={item.status} />
                        </td>
                      </tr>
                    ))
                  )}
                </tbody>
              </table>
            </div>

            <h3>审计</h3>
            <div className="table-wrap audit-table">
              <table>
                <thead>
                  <tr>
                    <th>时间</th>
                    <th>动作</th>
                    <th>详情</th>
                    <th>IP</th>
                  </tr>
                </thead>
                <tbody>
                  {data.audit.length === 0 ? (
                    <TableEmpty colSpan={4} />
                  ) : (
                    data.audit.map((item) => (
                      <tr key={item.id}>
                        <td>{formatDate(item.createdAt)}</td>
                        <td>{item.action}</td>
                        <td className="detail-cell">{item.detail}</td>
                        <td>{item.clientIp}</td>
                      </tr>
                    ))
                  )}
                </tbody>
              </table>
            </div>

            {!['ended', 'failed', 'expired'].includes(data.room.state) && (
              <div className="form-actions">
                <button className="danger" onClick={() => void endRoom()}>
                  结束课堂
                </button>
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}
