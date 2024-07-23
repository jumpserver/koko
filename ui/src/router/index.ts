import { createRouter, createWebHashHistory } from 'vue-router';
import type { RouteRecordRaw, Router } from 'vue-router';

const allRoutes: RouteRecordRaw[] = [
	{
		path: '/connect/',
		name: 'Terminal',
		component: () => import('@/views/Connection.vue'),
	},
	{
		path: '/token/',
		name: 'TokenParams',
		component: () => import('@/views/Connection.vue'),
	},
	{
		path: '/ks/',
		name: 'kubernetes',
		component: () => import('@/views/Kubernetes.vue'),
	},
	{
		path: '/token/:id/',
		name: 'Token',
		component: () => import('@/views/Connection.vue'),
	},
	{
		path: '/share/:id/',
		name: 'Share',
		component: () => import('@/views/ShareTerminal.vue'),
	},
	{
		path: '/monitor/:id/',
		name: 'Monitor',
		component: () => import('@/views/Monitor.vue'),
	},
];

const router: Router = createRouter({
	history: createWebHashHistory('/koko/'),
	routes: allRoutes,
	scrollBehavior: () => ({ left: 0, top: 0 }),
});

export default router;
