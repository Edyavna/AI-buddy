import tseslint from 'typescript-eslint';

export default tseslint.config(
  { ignores: ['src/shared/api/generated/**', 'coverage/**', 'public/mockServiceWorker.js'] },
  ...tseslint.configs.recommended,
);
