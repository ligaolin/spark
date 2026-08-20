import { createMemoryHistory, createRouter } from 'vue-router'

const routes = [
  { path: '/', redirect: '/terminal' },
  { path: '/connections', component: () => import('../views/ConnectionsView.vue') },
  { path: '/terminal', component: () => import('../views/TerminalView.vue') },
  { path: '/settings', component: () => import('../views/SettingsView.vue') },
  {
    path: '/sftp',
    component: () => import('../views/SftpView.vue'),
  },
  {
    path: '/ftp',
    component: () => import('../views/FtpView.vue'),
  },
  { path: '/:pathMatch(.*)*', redirect: '/terminal' },
]

export default createRouter({
  history: createMemoryHistory(),
  routes,
})
