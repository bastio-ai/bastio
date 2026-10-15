package detection

import "regexp"

// A replacement heading, salutation or transcript is not itself a request
// to cross the model's trust boundary. These expressions require a target.
const hostileDirective = `(?:\b(?:ignore|disregard|override|bypass|disable|forget)\b[^.?!\n]{0,70}\b(?:system|developer|previous|prior|above|safety|filters?|instructions?|prompts?|rules?|safeguards)\b|\b(?:reveal|exfiltrate|send|leak|upload|transmit|forward)\b[^.?!\n]{0,70}\b(?:system\s+prompt|hidden\s+instructions?|secrets?|credentials?|api\s+keys?)\b|\byou\s+(?:are|will\s+act\s+as)\s+(?:now\s+)?(?:an?\s+)?(?:unrestricted|uncensored|unfiltered|jailbroken|DAN)\b)`

var hostileInstruction = regexp.MustCompile(`(?i)` + hostileDirective)
var replacementInstructions = regexp.MustCompile(`(?i)\b(?:new|updated|revised)\s+(?:rules?|instructions?|guidelines?|system\s+prompt)\s*[:=.]\s*[^.?!\n]{0,180}` + hostileDirective)
