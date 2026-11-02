// IM review (the items merge): the chat's earlier turn (X59) and the found items (I4) together.
import type { ItemForm } from "./logic";
import type { Earlier } from "./voice";

/** The earlier turn after found items joined the list: the items join its list too (as turnsWithFound does for the
 *  turns), so earlierToSend still sends the question asked before the find. None stays none. */
export function earlierWithFound(e: Earlier | null, added: ItemForm[]): Earlier | null {
  return e && added.length ? { ...e, list: { ...e.list, items: [...e.list.items, ...added] } } : e;
}
