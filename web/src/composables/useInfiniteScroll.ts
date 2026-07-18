import { onBeforeUnmount, onMounted, type Ref, watch } from 'vue'

export function useInfiniteScroll(
  sentinel: Ref<HTMLElement | null>,
  onLoadMore: () => void | Promise<void>,
  enabled: Ref<boolean>,
) {
  let observer: IntersectionObserver | null = null

  function setup() {
    observer?.disconnect()
    if (!enabled.value || !sentinel.value) return
    observer = new IntersectionObserver(
      (entries) => {
        if (entries[0]?.isIntersecting) void onLoadMore()
      },
      { rootMargin: '240px' },
    )
    observer.observe(sentinel.value)
  }

  onMounted(setup)
  watch([sentinel, enabled], setup)
  onBeforeUnmount(() => observer?.disconnect())
}
