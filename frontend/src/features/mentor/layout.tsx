import { Navigate, Outlet } from 'react-router-dom';
import { useMe } from '@/features/session';

export function RoleHome() {
  const me = useMe();
  return <Navigate to={me.data?.role === 'mentor' ? '/mentor/questions' : '/onboarding'} replace />;
}
export function MentorGate() {
  const me = useMe();
  return me.data?.role === 'mentor' ? <Outlet /> : <Navigate to="/onboarding" replace />;
}
export function InternGate({ children }: { children: React.ReactNode }) {
  const me = useMe();
  return me.data?.role === 'mentor' ? <Navigate to="/mentor/questions" replace /> : children;
}
