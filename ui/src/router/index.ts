import type { Router, RouteRecordRaw } from 'vue-router';

import { createRouter, createWebHistory } from 'vue-router';

import { guard } from '../utils/guard';

const allRoutes: RouteRecordRaw[] = [
  {
    path: '/connect/',
    name: 'Terminal',
    component: () => import('@/pages/connection/index.vue'),
  },
  {
    path: '/sftp/:id/',
    name: 'SFTP',
    component: () => import('@/pages/file/index.vue'),
  },
  {
    path: '/token/',
    name: 'TokenParams',
    component: () => import('@/pages/connection/index.vue'),
  },
  {
    path: '/k8s/',
    name: 'kubernetes',
    component: () => import('@/pages/kubernetes/index.vue'),
  },
  {
    path: '/token/:id/',
    name: 'Token',
    component: () => import('@/pages/connection/index.vue'),
  },
  {
    path: '/share/:id/',
    name: 'Share',
    component: () => import('@/pages/share/index.vue'),
  },
  {
    path: '/monitor/:id/',
    name: 'Monitor',
    component: () => import('@/pages/monitor/index.vue'),
  },
  {
    path: '/sftp/',
    name: 'SFTP',
    component: () => import('@/pages/file/index.vue'),
  },
];

const router: Router = createRouter({
  history: createWebHistory('/koko/'),
  routes: allRoutes,
  scrollBehavior: () => ({ left: 0, top: 0 }),
});

router.beforeEach((_to, _from, next) => {
  guard(next);
});

export default router;
