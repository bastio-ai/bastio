import { useCallback, useSyncExternalStore } from "react"

type Theme = "light" | "dark"

const STORAGE_KEY = "bastio-theme"

function getStoredTheme(): Theme | null {
  const stored = localStorage.getItem(STORAGE_KEY)
  return stored === "light" || stored === "dark" ? stored : null
}

/**
 * Resolved theme on first boot.
 * Dark is the brand — always default to dark unless the user has explicitly
 * chosen light. System preference is deliberately ignored.
 */
function getResolvedTheme(): Theme {
  return getStoredTheme() ?? "dark"
}

function applyTheme(theme: Theme) {
  document.documentElement.classList.toggle("dark", theme === "dark")
}

let listeners: Array<() => void> = []

function subscribe(listener: () => void) {
  listeners = [...listeners, listener]
  return () => {
    listeners = listeners.filter((l) => l !== listener)
  }
}

function emitChange() {
  for (const listener of listeners) {
    listener()
  }
}

function getSnapshot(): Theme {
  return document.documentElement.classList.contains("dark") ? "dark" : "light"
}

export function useTheme() {
  const theme = useSyncExternalStore(subscribe, getSnapshot)

  const setTheme = useCallback((next: Theme) => {
    localStorage.setItem(STORAGE_KEY, next)
    applyTheme(next)
    emitChange()
  }, [])

  const toggleTheme = useCallback(() => {
    setTheme(getSnapshot() === "dark" ? "light" : "dark")
  }, [setTheme])

  return { theme, setTheme, toggleTheme } as const
}

// Called once at startup to sync DOM with stored/system preference
export function initTheme() {
  applyTheme(getResolvedTheme())
}
