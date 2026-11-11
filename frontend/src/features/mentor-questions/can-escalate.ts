export interface MessageEscalationState {
  role: 'user' | 'assistant';
  status: 'generating' | 'completed' | 'failed' | 'interrupted';
  outcome?: 'unknown' | 'answered' | 'needs_clarification' | 'knowledge_gap' | undefined;
}

export function canEscalateMessage(message: MessageEscalationState): boolean {
  return message.role === 'assistant'
    && message.status === 'completed'
    && message.outcome === 'knowledge_gap';
}
