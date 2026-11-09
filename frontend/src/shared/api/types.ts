import type { components, operations } from './generated/schema';

export type User = components['schemas']['User'];
export type Step = components['schemas']['Step'];
export type Plan = components['schemas']['Plan'];
export type Conversation = components['schemas']['Conversation'];
export type Message = components['schemas']['Message'];
export type Source = components['schemas']['Source'];
export type AccessRequest = components['schemas']['AccessRequest'];
export type ApiError = components['schemas']['ApiError'];
export type ChatEvent = components['schemas']['ChatEvent'];
export type CreateConversation = components['schemas']['CreateConversation'];
export type SendMessage = components['schemas']['SendMessage'];
export type ConfirmRequest = components['schemas']['ConfirmRequest'];
export type UpdateStep = components['schemas']['UpdateStep'];
export type Document = operations['getDocument']['responses'][200]['content']['application/json'];
