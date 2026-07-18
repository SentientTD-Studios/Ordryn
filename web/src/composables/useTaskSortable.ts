import { nextTick, onBeforeUnmount, type Ref, watch } from 'vue'
import Sortable from 'sortablejs'

type ReorderHandler = (taskIds: number[], favorite: boolean) => void | Promise<void>

export function useTaskSortable(
  favoriteListEl: Ref<HTMLElement | null>,
  taskListEl: Ref<HTMLElement | null>,
  enabled: Ref<boolean>,
  showFavorites: Ref<boolean>,
  onReorder: ReorderHandler,
) {
  let favSortable: Sortable | null = null
  let regSortable: Sortable | null = null

  function destroy() {
    favSortable?.destroy()
    regSortable?.destroy()
    favSortable = null
    regSortable = null
  }

  function createOptions(favorite: boolean): Sortable.Options {
    const coarse = window.matchMedia('(pointer: coarse)').matches
    return {
      handle: '.drag-handle',
      draggable: '.task-row',
      animation: 150,
      delay: coarse ? 200 : 0,
      delayOnTouchOnly: true,
      touchStartThreshold: coarse ? 5 : 1,
      onEnd(evt) {
        const tbody = evt.to as HTMLElement
        const ids = Array.from(tbody.querySelectorAll('tr.task-row'))
          .map((row) => parseInt(row.id.replace('task-', ''), 10))
          .filter((id) => !Number.isNaN(id))
        void onReorder(ids, favorite)
      },
    }
  }

  function init() {
    destroy()
    if (!enabled.value) return

    if (showFavorites.value && favoriteListEl.value) {
      favSortable = Sortable.create(favoriteListEl.value, createOptions(true))
    }
    if (taskListEl.value) {
      regSortable = Sortable.create(taskListEl.value, createOptions(false))
    }
  }

  watch([enabled, showFavorites, favoriteListEl, taskListEl], () => {
    void nextTick(init)
  })

  onBeforeUnmount(destroy)

  return { refresh: () => void nextTick(init), destroy }
}
