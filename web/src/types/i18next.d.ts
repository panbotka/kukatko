import 'i18next'

import type { defaultNS, resources } from '../i18n'

// Make the t() function aware of our resource keys so translation keys are
// type-checked and autocompleted, and unknown keys become compile errors.
declare module 'i18next' {
  interface CustomTypeOptions {
    defaultNS: typeof defaultNS
    resources: (typeof resources)['cs']
  }
}
