/* eslint-disable no-unused-vars */
import router from './router'
import store from '../store'

function onI18nLoaded() {
  return new Promise(resolve => {
    const load = store.state.i18nLoaded
    if (load) {
      resolve()
    }
    const itv = setInterval(() => {
      const load = store.state.i18nLoaded
      if (load) {
        clearInterval(itv)
        resolve()
      }
    }, 100)
  })
}

export async function startup({ to, from, next }) {
  if (store.getters.inited) {
    return true
  }
  await store.dispatch('init')
  await onI18nLoaded()
  return true
}

router.beforeEach(async(to, from, next) => {
  try {
    await startup({ to, from, next })
    next()
  } catch (e) {
    console.log('Start service error: ' + e)
  }
})


