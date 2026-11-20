"use client";

const LEGACY_KEYS: Record<string, string> = {
  "reconrivet.chat.collapsed-agents": "artex.chat.collapsed-agents",
  reconrivet_chat_send_mode: "artex_chat_send_mode",
  reconrivet_finding_list_preferences: "artex_finding_list_preferences",
  reconrivet_task_list_sort: "artex_task_list_sort",
  reconrivet_task_list_filters: "artex_task_list_filters",
  reconrivet_task_findings_sort: "artex_task_findings_sort",
  "reconrivet-update-dismissed": "artex-update-dismissed",
};

export function setLocalStorageValue(key: string, value: string) {
  try {
    window.localStorage.setItem(key, value);
  } catch (error) {
    if (process.env.NODE_ENV !== "production") {
      console.error("[localStorage] Failed to write value:", error);
    }
  }
}

export function getLocalStorageValue(key: string): string | null {
  try {
    const current = window.localStorage.getItem(key);
    if (current !== null) return current;
    const legacyKey = LEGACY_KEYS[key];
    if (!legacyKey) return null;
    const legacy = window.localStorage.getItem(legacyKey);
    if (legacy !== null) {
      try {
        window.localStorage.setItem(key, legacy);
        window.localStorage.removeItem(legacyKey);
      } catch {
        // Keep the preference available if a browser allows reads but blocks writes.
      }
    }
    return legacy;
  } catch {
    return null;
  }
}
