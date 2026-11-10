import { http, HttpResponse } from 'msw';
import { createScenario } from '../fixtures/scenario';

// Explicit local preview only. FE2 production session/plan modules are not replaced.
export function createPreviewHandlers() {
  const fixture = createScenario();
  return [
    http.get('*/api/v1/me', () => HttpResponse.json(fixture.user)),
    http.get('*/api/v1/onboarding/plan', () => HttpResponse.json(fixture.plan)),
  ];
}
