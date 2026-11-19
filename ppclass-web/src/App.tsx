import { Navigate, Route, Routes } from 'react-router-dom';
import ClassroomPage from './pages/ClassroomPage';
import JoinPage from './pages/JoinPage';
import LoginPage from './pages/LoginPage';
import TeacherHomePage from './pages/TeacherHomePage';

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route path="/" element={<TeacherHomePage />} />
      <Route path="/join" element={<JoinPage />} />
      <Route path="/classroom/:roomId" element={<ClassroomPage />} />
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
