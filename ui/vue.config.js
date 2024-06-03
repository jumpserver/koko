module.exports = {
  publicPath: '/koko/',
  outputDir: 'dist',
  assetsDir: 'assets',
  devServer: {
    port: 9530,
    proxy: {
      '^/koko/ws/': {
        target: 'http://127.0.0.1:5001/',
        ws: true,
        changeOrigin: true
      },
      '^/api/': {
        target: 'http://127.0.0.1:8080/',
        ws: true,
        changeOrigin: true
      }
    }
  },
}
