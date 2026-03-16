import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import { defineConfig, globalIgnores } from 'eslint/config'

export default defineConfig([
  globalIgnores(['dist']),
  {
    files: ['**/*.{js,jsx}'],
    extends: [
      js.configs.recommended,
      reactHooks.configs.flat.recommended,
      reactRefresh.configs.vite,
    ],
    languageOptions: {
      ecmaVersion: 2020,
      globals: globals.browser,
      parserOptions: {
        ecmaVersion: 'latest',
        ecmaFeatures: { jsx: true },
        sourceType: 'module',
      },
    },
    rules: {
      'no-unused-vars': ['error', { varsIgnorePattern: '^[A-Z_]' }],
      // Allow exporting hooks alongside components (standard Context + Hook pattern)
      // and allow exporting variant helpers alongside components (shadcn/ui pattern)
      'react-refresh/only-export-components': [
        'warn',
        { allowExportNames: ['useConnection'], allowConstantExport: true },
      ],
    },
  },
  // Node.js globals for config files
  {
    files: ['vite.config.js'],
    languageOptions: {
      globals: globals.node,
    },
  },
])
