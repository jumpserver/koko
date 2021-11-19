import Vue from 'vue'
import Router from 'vue-router'

Vue.use(Router)

export const allRoleRoutes = [
  {
    path: '/terminal/',
    name: 'Terminal',
    component: () => import('../views/Connection')
  },
  {
    path: '/token/',
    name: 'TokenParams',
    component: () => import('../views/Connection')
  },
  {
    path: '/token/:id/',
    name: 'Token',
    component: () => import('../views/Connection')
  },
  {
    path: '/share/:id/',
    name: 'Share',
    component: () => import('../views/ShareTerminal')
  },
  {
    path: '/monitor/:id/',
    name: 'Monitor',
    component: () => import('../views/Monitor')
  }
]

const createRouter = () => new Router({
  mode: 'history', // require service support
  // scrollBehavior: () => ({y: 0}),
  base: '/koko/',
  routes: allRoleRoutes
})

const router = createRouter()

// Detail see: https://github.com/vuejs/vue-router/issues/1234#issuecomment-357941465
export function resetRouter() {
  const newRouter = createRouter()
  router.matcher = newRouter.matcher // reset router
}

export default router
