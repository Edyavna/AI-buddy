import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import path from 'path';

// Сквозной сценарий через реальные App + router + stateful MSW-имитацию.
// Запуск: npm run test:e2e. Покрытие здесь не считаем — оно настроено в vitest.config.ts.
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  test: {
    globals: true,
    environment: 'jsdom',
    environmentOptions: { jsdom: { url: 'http://localhost/' } },
    clearMocks: true,
    setupFiles: ['./src/test/setup.ts'],
    css: true,
    include: ['e2e/**/*.test.tsx'],
  },
});
