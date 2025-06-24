import antfu from '@antfu/eslint-config';

export default antfu({
  stylistic: {
    indent: 2,
    quotes: 'single',
    semi: true,
  },

  typescript: true,
  vue: true,

  ignores: [
    'src/style/font/**/*',
  ],
});
