// X35e: web voice capture. expo-audio 58.0.10's useAudioStream is a stub on
// the web (build/AudioStream.web.js returns stream: null), so the web app
// captures the microphone with the Web Audio API: this AudioWorkletProcessor
// copies channel 0 of each 128-frame block and posts it to the main thread
// (MDN: AudioWorkletNode.port). src/voicerec.ts makes the WAV.
class VoiceCapture extends AudioWorkletProcessor {
  process(inputs) {
    const ch = inputs[0] && inputs[0][0];
    if (ch && ch.length) this.port.postMessage(ch.slice(0));
    return true;
  }
}
registerProcessor("voice-capture", VoiceCapture);
