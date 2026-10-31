// R5 (T9 2026-10-31): the rescue of an expired listing nobody grabbed. A popup over "Your listings" that starts
// the rescue job (post/rescue.go, ?async=1), waits for it, and shows up to 3 places near the pickup address:
// directions and a call link for each; for bulk food (30 servings or more) also an email link (the drafted
// message). mobile/docs/style_guide.md "Rescue".
import { useEffect, useRef, useState } from "react";
import { Linking, Platform, ScrollView, Text, View } from "react-native";
import { ApiError, rescue as api, type Listing } from "../api";
import { needsZip, placeLine, rescueView, safeLink, zipOf, type Rescue, type RescuePlace } from "../rescue";
import { useColors, radius, size, space, type } from "../theme";
import { Body, ErrorText, Field, Muted, Pill, Popup, Row, TextLink, Title } from "../ui";
import { RESCUE } from "../words";
import { useSession } from "../session";

/** Opens a link outside the app: a new browser tab on the web. */
function openOut(url: string) {
  if (!url) return;
  if (Platform.OS === "web") window.open(url, "_blank", "noopener");
  else Linking.openURL(url).catch(() => undefined);
}

function PlaceRow({ p, bulk }: { p: RescuePlace; bulk: boolean }) {
  const col = useColors();
  const dir = safeLink(p.directions, "https"), tel = safeLink(p.tel, "tel"), mail = bulk ? safeLink(p.mailto, "mailto") : "";
  return (
    <View testID={"place-" + p.id} style={{ backgroundColor: col.card, borderRadius: radius.card, borderWidth: size.border, borderColor: col.line,
      padding: space.group, gap: space.hair }}>
      <Text style={[type.button, { color: col.fg }]}>{p.name}</Text>
      <Muted>{placeLine(p)}</Muted>
      {p.address ? <Muted lines={2}>{p.address}</Muted> : null}
      <Row tight style={{ gap: space.gutter }}>
        {dir ? <TextLink testID={"dir-" + p.id} name={RESCUE.linkName(RESCUE.directions, p.name)} touch onPress={() => openOut(dir)}>{RESCUE.directions}</TextLink> : null}
        {tel ? <TextLink testID={"tel-" + p.id} name={RESCUE.linkName(RESCUE.call, p.name)} touch onPress={() => openOut(tel)}>{RESCUE.call}</TextLink> : null}
        {mail ? <TextLink testID={"mail-" + p.id} name={RESCUE.linkName(RESCUE.email, p.name)} touch onPress={() => openOut(mail)}>{RESCUE.email}</TextLink> : null}
      </Row>
    </View>
  );
}

export default function RescueScreen({ listing, onClose }: { listing: Listing; onClose: () => void }) {
  const [res, setRes] = useState<Rescue | null>(null);
  const [problem, setProblem] = useState(""); // a failure of the request itself
  const [askZip, setAskZip] = useState(false);
  const [zip, setZip] = useState("");
  const [zipError, setZipError] = useState(false);
  const { ended } = useSession();
  const closed = useRef(false);
  const current = useRef(0); // R5 review: only the newest run may change the popup
  const [busy, setBusy] = useState(false); // a run is active: Search and Try again wait
  const run = async (z = "") => {
    if (current.current > 0 && busy) return;
    const mine = ++current.current;
    const stale = () => closed.current || mine !== current.current;
    setProblem(""); setRes(null); setBusy(true);
    try {
      const first = await api.start(listing.id, z);
      if (stale()) return;
      setAskZip(false);
      setRes(first);
      const last = await api.wait(first, stale);
      if (stale()) return;
      setRes(last);
    } catch (err) {
      if (stale()) return;
      const status = err instanceof ApiError ? err.status : 0, msg = err instanceof Error ? err.message : String(err);
      if (status === 401) { closed.current = true; onClose(); ended(); return; } // R5 review: signed out
      if (needsZip(status, msg)) { setAskZip(true); return; }
      setProblem(RESCUE.failed);
    } finally {
      if (!stale()) setBusy(false);
    }
  };
  useEffect(() => { run(); return () => { closed.current = true; }; }, []); // eslint-disable-line react-hooks/exhaustive-deps
  const close = () => { closed.current = true; onClose(); };
  const search = () => {
    if (busy) return;
    const z = zipOf(zip);
    setZipError(!z);
    if (z) run(z);
  };
  const view = problem ? "failed" : askZip ? "zip" : rescueView(res);
  const bulk = res?.mode === "email";
  return (
    <Popup testID="rescue" small onDismiss={close} dismissName={RESCUE.close}>
      <Title style={{ marginBottom: space.hair }}>{view !== "places" ? RESCUE.find : bulk ? RESCUE.titleBulk : RESCUE.title}</Title>
      <Muted lines={2} style={{ marginBottom: space.gutter }}>{listing.title}</Muted>
      {view === "looking" ? <Muted>{RESCUE.looking}</Muted> : null}
      {view === "zip" ? (
        <View style={{ gap: space.gap }}>
          <Muted>{RESCUE.zipAsk}</Muted>
          <Field testID="rescue-zip" label={RESCUE.zip} placeholder={RESCUE.zipHint} value={zip} onChangeText={setZip}
            keyboardType="number-pad" maxLength={10} onSubmitEditing={search} />
          {zipError ? <ErrorText>{RESCUE.zipBad}</ErrorText> : null}
          <Pill testID="rescue-search" title={RESCUE.search} busy={busy} onPress={search} />
        </View>
      ) : null}
      {view === "places" && res ? (
        <ScrollView style={{ flexShrink: 1 }} contentContainerStyle={{ gap: space.item }}>
          <Body>{bulk ? RESCUE.bulk : RESCUE.dropoff}</Body>
          {res.places.map((p) => <PlaceRow key={p.id} p={p} bulk={bulk} />)}
          {res.attribution ? <Muted>{res.attribution}</Muted> : null}
        </ScrollView>
      ) : null}
      {view === "none" ? <Muted>{RESCUE.none}</Muted> : null}
      {view === "failed" ? (
        <View style={{ gap: space.gap }}>
          <ErrorText>{problem || RESCUE.failed}</ErrorText>
          <Pill testID="rescue-retry" title={RESCUE.retry} busy={busy} onPress={() => run(zipOf(zip))} />
        </View>
      ) : null}
      <Row style={{ marginTop: space.gutter }}>
        <Pill testID="rescue-close" kind="secondary" title={RESCUE.close} grow onPress={close} />
      </Row>
    </Popup>
  );
}
