exports.handler = function (context, event, callback) {
  const twiml = new Twilio.twiml.VoiceResponse();
  let audioUrl = "";
  let sayTestMessage = false;
  const accountId = Number.parseInt(event.id, 10);
  const audio = String(event.audio || "").toUpperCase();
  const testMessage =
    "this is a test of the thundercall alerting system. Reminder this is just a test.";

  if (accountId === 64623) {
    switch (audio) {
      case "WSW":
        audioUrl = "https://thundercall-2287.twil.io/thundercall%20winter.mp3";
        break;
      case "FFW":
        audioUrl = "https://thundercall-2287.twil.io/Thundercall%20flood.mp3";
        break;
      case "TOR":
        audioUrl = "https://thundercall-2287.twil.io/Thundercall%20Tornado.mp3";
        break;
      case "SVR":
        audioUrl = "https://thundercall-2287.twil.io/Thundercall%20Tstorm.mp3";
        break;
      case "TEST":
        sayTestMessage = true;
        break;
      default:
        audioUrl = "https://thundercall-2287.twil.io/DefaultThunderCall.mp3";
        break;
    }
  } else if (accountId === 64622) {
    switch (audio) {
      case "WSW":
        audioUrl = "https://thundercall-2287.twil.io/WinterKWTX.mp3";
        break;
      case "FFW":
        audioUrl = "https://thundercall-2287.twil.io/FlashFloodKWTX.mp3";
        break;
      case "TOR":
        audioUrl = "https://thundercall-2287.twil.io/TornadoKWTX.mp3";
        break;
      case "SVR":
        audioUrl = "https://thundercall-2287.twil.io/SevereWeatherKWTX.mp3";
        break;
      case "TEST":
        sayTestMessage = true;
        break;
      default:
        audioUrl = "https://thundercall-2287.twil.io/DefaultThunderCall.mp3";
        break;
    }
  } else if (
    accountId === 64624 ||
    accountId === 30411 ||
    accountId === 59907 ||
    accountId === 47356
  ) {
    switch (audio) {
      case "WSW":
        audioUrl = "https://thundercall-2287.twil.io/KTREWinterStormWarning.mp3";
        break;
      case "FFW":
        audioUrl = "https://thundercall-2287.twil.io/KTREFloodWarning.mp3";
        break;
      case "TOR":
        audioUrl = "https://thundercall-2287.twil.io/KTRETornadoWarning.mp3";
        break;
      case "SVR":
        audioUrl = "https://thundercall-2287.twil.io/KTREThunderStormWarning.mp3";
        break;
      case "TEST":
        sayTestMessage = true;
        break;
      default:
        audioUrl = "https://thundercall-2287.twil.io/DefaultThunderCall.mp3";
        break;
    }
  } else {
    audioUrl = "https://thundercall-2287.twil.io/DefaultThunderCall.mp3";
  }

  if (sayTestMessage) {
    twiml.say(testMessage);
  } else {
    twiml.play(audioUrl);
  }

  twiml.pause({ length: 1 });
  twiml.hangup();
  callback(null, twiml);
};
