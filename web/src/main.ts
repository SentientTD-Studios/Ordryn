import { createApp } from 'vue'
import App from './App.vue'
import router from './router'
import { installStaleChunkReload } from '@/utils/staleChunk'
import 'bootstrap/dist/js/bootstrap.bundle.min.js'
import './style.css'

installStaleChunkReload(router)
createApp(App).use(router).mount('#app')
