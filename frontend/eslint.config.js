import tseslint from 'typescript-eslint';

export default tseslint.config(
  { ignores: ['src/shared/api/generated/**', 'coverage/**', 'dist/**', 'public/mockServiceWorker.js', '.taste-skill/**'] },
  ...tseslint.configs.recommended,
);
