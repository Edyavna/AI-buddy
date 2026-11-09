import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    environment: 'jsdom',
    environmentOptions: { jsdom: { url: 'http://localhost/' } },
    setupFiles: ['./src/test/setup.ts'],
    clearMocks: true,
    coverage: {
      provider: 'v8',
      include: ['src/**/*.{ts,tsx}'],
      exclude: ['src/**/*.test.{ts,tsx}', 'src/shared/api/generated/**', 'src/mocks/**', 'src/test/**'],
      reporter: ['text', 'html', 'lcov'],
      thresholds: { lines: 81 },
    },
  },
});
