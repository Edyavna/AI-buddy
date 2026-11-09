import React from 'react';
import ReactDOM from 'react-dom/client';
import { App } from './app/App';

async function bootstrap() {
  if (import.meta.env.DEV && import.meta.env.VITE_API_MOCKING === 'enabled') {
    const { startBrowserMocks } = await import('./mocks/browser');
    await startBrowserMocks({ development: true, enabled: true });
  }

  const rootElement = document.getElementById('root');
  if (rootElement) {
    ReactDOM.createRoot(rootElement).render(
      <React.StrictMode><App /></React.StrictMode>,
    );
  }
}

void bootstrap().catch((error: unknown) => {
  console.error('Application startup failed', error);
  const rootElement = document.getElementById('root');
  if (rootElement) rootElement.textContent = 'Не удалось запустить приложение. Обновите страницу.';
});
