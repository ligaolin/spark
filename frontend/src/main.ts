import { createApp } from 'vue'
import App from './App.vue'
import { createPinia } from 'pinia'
import piniaPluginPersistedstate from 'pinia-plugin-persistedstate'
import router from './utils/router'
import 'element-plus/theme-chalk/dark/css-vars.css'
// 编程式组件（ElMessage 等）不走按需导入的样式注入，这里显式引入其样式
import 'element-plus/es/components/message/style/css'
import 'element-plus/es/components/message-box/style/css'
import './styles.css'

// 启用 Element Plus 暗色主题
document.documentElement.classList.add('dark')

// 禁用浏览器（WebView2）内置右键菜单：桌面应用内右键菜单由应用自身提供。
// 用捕获阶段监听，确保在任何元素处理器（含 stopPropagation）之前就
// preventDefault，避免行右键等场景漏掉导致系统菜单弹出。
window.addEventListener('contextmenu', (e) => e.preventDefault(), true)

createApp(App)
  .use(createPinia().use(piniaPluginPersistedstate))
  .use(router)
  .mount('#app')
