import { RECEIVE, timeText, postTitle } from "../words";
import { RoleSheet } from "./Role";
export function Give() { return <View><Shutter /><RoleSheet /><Text>{timeText(l, RECEIVE)}</Text><Pill title={postTitle(2)} /></View>; }
