import type { User } from '@/shared/api';

export const roleHome: Record<User['role'], string> = {
  intern: '/onboarding',
  mentor: '/mentor/questions',
  hr: '/hr/questions',
};

export const roleLabel: Record<User['role'], string> = {
  intern: 'Стажёр',
  mentor: 'Наставник',
  hr: 'HR',
};
