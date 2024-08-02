import { guard } from './helper/guard';
import { createRouter, createWebHistory, RouteRecordRaw, Router } from 'vue-router';

const allRoutes: RouteRecordRaw[] = [
  {
    path: '/connect/',
    name: 'Terminal',
    component: () => import(/* webpackChunkName: Connect */ '@/views/Connection.vue')
  },
  {
    path: '/token/',
    name: 'TokenParams',
    component: () => import(/* webpackChunkName: Token */ '@/views/Connection.vue')
  },
  {
    path: '/ks/',
    name: 'kubernetes',
    component: () => import(/* webpackChunkName: Ks */ '@/views/Kubernetes.vue')
  },
  {
    path: '/token/:id/',
    name: 'Token',
    component: () => import(/* webpackChunkName: TokenId */ '@/views/Connection.vue')
  },
  {
    path: '/share/:id/',
    name: 'Share',
    component: () => import(/* webpackChunkName: Share */ '@/views/ShareTerminal.vue')
  },
  {
    path: '/monitor/:id/',
    name: 'Monitor',
    component: () => import(/* webpackChunkName: Monitor */ '@/views/Monitor.vue')
  }
];

const router: Router = createRouter({
  history: createWebHistory('/koko/'),
  routes: allRoutes,
  scrollBehavior: () => ({ left: 0, top: 0 })
});

router.beforeEach(async (_to, _from, next) => {
  await guard(next);
});

export default router;
