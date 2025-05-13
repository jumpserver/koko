import { guard } from '../utils/guard';
import { createRouter, createWebHistory, RouteRecordRaw, Router } from 'vue-router';

const allRoutes: RouteRecordRaw[] = [
  {
    path: '/connect/',
    name: 'Terminal',
    component: () => import('@/views/connection/index.vue')
  },
  {
    path: '/sftp/:id/',
    name: 'SFTP',
    component: () => import('@/views/file/index.vue')
  },
  {
    path: '/token/',
    name: 'TokenParams',
    component: () => import('@/views/connection/index.vue')
  },
  {
    path: '/k8s/',
    name: 'kubernetes',
    component: () => import('@/views/kubernetes/index.vue')
  },
  {
    path: '/token/:id/',
    name: 'Token',
    component: () => import('@/views/connection/index.vue')
  },
  {
    path: '/share/:id/',
    name: 'Share',
    component: () => import('@/views/share/index.vue')
  },
  {
    path: '/monitor/:id/',
    name: 'Monitor',
    component: () => import('@/views/monitor/index.vue')
  },
  {
    path: '/sftp',
    name: 'SFTP',
    component: () => import('@/views/file/index.vue')
  },
];

const router: Router = createRouter({
  history: createWebHistory('/koko/'),
  routes: allRoutes,
  scrollBehavior: () => ({ left: 0, top: 0 })
});

router.beforeEach((_to, _from, next) => {
  guard(next);
});

export default router;
