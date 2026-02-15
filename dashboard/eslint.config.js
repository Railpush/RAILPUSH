import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import tseslint from 'typescript-eslint'
import { defineConfig, globalIgnores } from 'eslint/config'

export default defineConfig([
  globalIgnores(['dist']),
  {
    files: ['**/*.{ts,tsx}'],
    extends: [
      js.configs.recommended,
      tseslint.configs.recommended,
      reactHooks.configs.flat.recommended,
      reactRefresh.configs.vite,
    ],
    rules: {
      // Useful in dev, but too restrictive for our current module patterns (contexts/hooks live together).
      'react-refresh/only-export-components': 'off',

      // These rules are opinionated and flag common patterns in this codebase.
      // We still keep rules-of-hooks + exhaustive-deps from react-hooks.
      'react-hooks/set-state-in-effect': 'off',
      'react-hooks/purity': 'off',
    },
    languageOptions: {
      ecmaVersion: 2020,
      globals: globals.browser,
    },
  },
])
