import { EmptyState } from '@/shared/ui';
function Pending({ title }: { title: string }) { return <section><h1>{title}</h1><EmptyState title="Раздел готовится" description="Здесь появятся данные кабинета наставника." /></section>; }
export const MentorInternsPage = () => <Pending title="Назначенные стажёры" />;
export const MentorInternPlanPage = () => <Pending title="План стажёра" />;
