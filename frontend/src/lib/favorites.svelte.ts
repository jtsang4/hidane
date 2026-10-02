import type { RunAs } from "./api.js";
import { loadFavorites, saveFavorites, toggleFavorite } from "./runAs.js";

/** Favorite combinations, shared by every picker in the window and kept on this machine. */
export const favorites = $state<{ list: RunAs[] }>({ list: loadFavorites() });

export function toggleFavoriteRun(value: RunAs): void {
  favorites.list = toggleFavorite(favorites.list, value);
  saveFavorites(favorites.list);
}
