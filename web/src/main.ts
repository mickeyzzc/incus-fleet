import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'

import App from './App.vue'
import router from './router'
import i18n from './i18n'
import './styles/main.css'

const app = createApp(App)

app.use(createPinia())
app.use(router)
app.use(i18n)
app.use(ElementPlus)

// Last-resort handlers so an uncaught failure is visible in the console and
// surfaceable in tests instead of vanishing; component-level handling stays
// the primary path.
app.config.errorHandler = (err, _instance, info) => {
  console.error('[fleet] unhandled component error:', info, err)
}
window.addEventListener('unhandledrejection', (event) => {
  console.error('[fleet] unhandled rejection:', event.reason)
})

app.mount('#app')
