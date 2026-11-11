export const queryKeys = {
  me: () => ['session', 'me'] as const,
  plan: () => ['onboarding', 'plan'] as const,
  conversations: () => ['chat', 'conversations'] as const,
  conversation: (id: string) => ['chat', 'conversation', id] as const,
  messages: (id: string) => ['chat', 'messages', id] as const,
  document: (id: string) => ['documents', id] as const,
  requests: () => ['access-requests', 'list'] as const,
  request: (id: string) => ['access-requests', 'detail', id] as const,
  mentorQuestion: (messageId: string) => ['mentor-questions', 'message', messageId] as const,
};
