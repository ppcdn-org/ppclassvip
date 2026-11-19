import { Navigate, Route, Routes } from 'react-router-dom';
import AdminLayout from './components/AdminLayout';
import AttachmentsPage from './pages/AttachmentsPage';
import AuditPage from './pages/AuditPage';
import ClassroomsPage from './pages/ClassroomsPage';
import DashboardPage from './pages/DashboardPage';
import LoginPage from './pages/LoginPage';
import RecordingsPage from './pages/RecordingsPage';
import SettingsPage from './pages/SettingsPage';
import UsersPage from './pages/UsersPage';

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route element={<AdminLayout />}>
        <Route index element={<DashboardPage />} />
        <Route path="users" element={<UsersPage />} />
        <Route path="classrooms" element={<ClassroomsPage />} />
        <Route path="recordings" element={<RecordingsPage />} />
        <Route path="attachments" element={<AttachmentsPage />} />
        <Route path="audit" element={<AuditPage />} />
        <Route path="settings" element={<SettingsPage />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
