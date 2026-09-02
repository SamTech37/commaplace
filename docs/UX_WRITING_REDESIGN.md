# Commaplace writing experience redesign

Branch: `ux/obsidian-writing-experience`

## Product direction

Commaplace should feel like an Obsidian writing workspace with a calmer entry
point than Notion and less chrome than Word. Markdown remains the source of
truth. Wiki links, tags, image upload, Markdown import, autosave, publishing,
search, graph, calendar, feed, profile, reader controls, and every other current
capability remain available.

No feature is removed as part of this redesign. A future removal needs a
separate proposal and explicit approval.

## Problems to solve

The current editor makes the toolbar the strongest visual object while its
glyph-only controls are difficult to identify. Its controls are too small for
touch, the writing canvas has no useful status bar, and the mobile layout puts
formatting far away from the thumb. Across the product, focus states and target
sizes are inconsistent, key navigation moves into hidden menus on small screens,
and secondary text can become needlessly difficult to scan.

Research across Obsidian, Notion, and Word user discussions shows recurring
failure modes:

- mobile interfaces that are desktop layouts squeezed into a phone;
- toolbars that obscure text or cannot be dismissed;
- selection, caret, or scroll position jumping during editing;
- slow or ambiguous saving that makes writers fear losing text;
- common actions hidden behind several clicks;
- formatting that changes without a deliberate user action;
- icon-only ribbons whose meaning must be guessed;
- large permanent ribbons that compete with the document.

Representative research inputs (reviewed 2026-09-03):

- [Obsidian mobile feels like a squeezed desktop interface](https://www.reddit.com/r/ObsidianMD/comments/1o74hpk/anyone_else_feels_obsidian_mobile_is_really/)
- [Obsidian editor and mobile friction](https://www.reddit.com/r/ObsidianMD/comments/1f3add6/your_most_annoying_problems_with_obsidian/)
- [Notion performance and search complaints](https://www.reddit.com/r/Notion/comments/1pedvtz/notion_is_horrifically_laggy_and_buggy_and/)
- [Notion interrupts long-form writing](https://www.reddit.com/r/Notion/comments/1pm6cka/this_is_absurd_and_the_first_time_ive_considered/)
- [Word users fighting document formatting](https://www.reddit.com/r/MicrosoftWord/comments/1ncb5og/word_formatting_drives_me_crazy_how_do_you_all/)
- [Word users asking to remove intrusive writing controls](https://answers.microsoft.com/en-us/msoffice/forum/all/how-to-disable-or-remove-copilot-from-word-there/5116e21b-c535-4b27-8061-ad84f43dfa97)

## Interaction model

### Editor

- The note is the visual focus; controls use the sans-serif UI face and prose
  keeps the reading face.
- The action bar reports draft/published state, save state, and the primary
  publish/update action without moving as the message changes.
- Formatting controls use familiar marks plus visible Traditional Chinese
  labels. Every control has an accessible name and a 44px touch target.
- On desktop, the formatting bar sits above a paper-like writing canvas. On
  mobile, it becomes a horizontally scrollable sticky bar near the thumb and
  never covers the caret.
- A quiet status row shows word and character counts. Markdown syntax remains
  visible but subordinate.
- Existing editor actions and document semantics are preserved.

### Navigation and global UI

- The top bar remains compact and sticky; primary destinations stay visible on
  desktop.
- Mobile gets a persistent bottom dock for Feed, Write, Search, and the signed-in
  user's vault. Less frequent destinations remain in the existing menu.
- Interactive targets are at least 44px on touch layouts, with consistent
  `:focus-visible` treatment for keyboard users.
- Menus and dialogs stay inside the viewport, use readable labels, and respect
  safe-area insets.
- Content widths follow purpose: prose stays narrow; feeds, graphs, calendars,
  and administrative data can use the available width.

## Acceptance criteria

1. Every current editor action is still present and works.
2. Editor controls have visible labels or an unambiguous conventional symbol,
   accessible names, keyboard focus, and 44px mobile targets.
3. At 360px wide, neither the editor nor global navigation creates horizontal
   page overflow; the formatting bar may scroll within itself.
4. The mobile formatting bar and bottom dock do not cover the final editable
   line, autocomplete, or focused form fields.
5. Autosave state is announced without layout shift, and word/character counts
   update as the document changes.
6. All text/background pairs meet WCAG AA under both themes, excluding disabled
   decoration and placeholder text.
7. Every page has a skip link and visible keyboard focus.
8. Templ generation, Go formatting, build, and available tests pass.
