import { Navigate, Outlet } from 'react-router-dom';
import { roleHome, useMe } from '@/features/session';

export function RoleHome() {
  const me = useMe();
  return <Navigate to={me.data ? roleHome[me.data.role] : '/login'} replace />;
}
export function MentorGate() {
  const me = useMe();
  return me.data?.role === 'mentor' ? <Outlet /> : <RoleHome />;
}
export function InternGate({ children }: { children: React.ReactNode }) {
  const me = useMe();
  return me.data?.role === 'intern' ? children : <RoleHome />;
}
