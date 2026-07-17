import { onMounted, ref } from 'vue'

const theme = ref<'light' | 'dark'>('light')

function readTheme(): 'light' | 'dark' {
  const existing = document.documentElement.getAttribute('data-theme')
  if (existing === 'dark' || existing === 'light') return existing
  try {
    const saved = localStorage.getItem('theme')
    if (saved === 'dark' || saved === 'light') return saved
  } catch {
    /* ignore */
  }
  return 'light'
}

function applyTheme(next: 'light' | 'dark') {
  theme.value = next
  document.documentElement.setAttribute('data-theme', next)
  try {
    localStorage.setItem('theme', next)
    document.cookie = `theme=${next}; path=/; max-age=31536000; SameSite=Lax`
  } catch {
    /* ignore */
  }
}

export function useTheme() {
  onMounted(() => {
    applyTheme(readTheme())
  })

  function toggleTheme() {
    applyTheme(theme.value === 'light' ? 'dark' : 'light')
  }

  const isDark = () => theme.value === 'dark'

  return { theme, toggleTheme, isDark }
}
