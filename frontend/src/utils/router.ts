import { createMemoryHistory, createRouter } from 'vue-router'

export const alwaysRoutes = [
  { path: '/', redirect: '/terminal' },
  { path: '/settings', component: () => import('../views/SettingsView.vue') },
  { path: '/:pathMatch(.*)*', redirect: '/terminal' },
]

export const toggleableRoutes = [
  { path: '/connections', component: () => import('../views/ConnectionsView.vue') },
  { path: '/terminal', component: () => import('../views/TerminalView.vue') },
  { path: '/sftp', component: () => import('../views/SftpView.vue') },
  { path: '/ftp', component: () => import('../views/FtpView.vue') },
  { path: '/documents', component: () => import('../views/DocumentsView.vue') },
  { path: '/sites', component: () => import('../views/SitesView.vue') },
  { path: '/remote-editor', component: () => import('../views/RemoteEditorView.vue') },
  { path: '/local-terminal', component: () => import('../views/LocalTerminalView.vue') },
  { path: '/rest', component: () => import('../views/RestView.vue') },
]

export default createRouter({
  history: createMemoryHistory(),
  routes: [...alwaysRoutes, ...toggleableRoutes],
})