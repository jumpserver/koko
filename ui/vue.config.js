module.exports = {
  publicPath: '/koko/',
  outputDir: 'dist',
  assetsDir: 'assets',
  devServer: {
    port: 9530,
    proxy: {
      '^/koko/ws/': {
        target: 'http://127.0.0.1:5000/',
        ws: true,
        changeOrigin: true
      }
    }
  },
  chainWebpack(config) {
  }
}
