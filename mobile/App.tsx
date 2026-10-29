// V3RDE app shell (NEW_WORK_UI.md, UI 5): it loads the brand font and shows the app name in the theme
// colours. The screens replace this body in the next changes.
import { StatusBar as SystemBar } from "expo-status-bar";
import { View } from "react-native";
import { useFonts as useFontFiles } from "expo-font";
import { Poppins_400Regular as regularFile } from "@expo-google-fonts/poppins/400Regular";
import { Poppins_600SemiBold as semiboldFile } from "@expo-google-fonts/poppins/600SemiBold";
import { font, useColors } from "./src/theme";
import { Muted, Title } from "./src/ui";

const Shell = () => {
  const [ready] = useFontFiles({ [font.regular]: regularFile, [font.semibold]: semiboldFile });
  const col = useColors();
  return (
    <View style={{ flex: 1, alignItems: "center", justifyContent: "center", backgroundColor: col.bg }}>
      {ready ? <><Title center>V3RDE</Title><Muted center>The screens come next.</Muted></> : null}
      <SystemBar style={"auto"} />
    </View>
  );
};
export default Shell;
