import antfu from '@antfu/eslint-config';

export default antfu(
  {
    stylistic: {
      indent: 2,
      quotes: 'single',
      semi: true,
    },

    typescript: true,
    vue: true,

    ignores: ['src/style/font/**/*'],
  },
  {
    rules: {
      'perfectionist/sort-imports': [
        'error',
        {
          type: 'line-length',
          order: 'asc',
          ignoreCase: true,
          newlinesBetween: 'always',
          groups: [
            'type',
            'builtin',
            'external',
            'internal-type',
            'internal',
            ['parent-type', 'sibling-type', 'index-type'],
            ['parent', 'sibling', 'index'],
            'object',
            'unknown',
          ],
        },
      ],
      'antfu/top-level-function': 'off',
    },
  },
);
