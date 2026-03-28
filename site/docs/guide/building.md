# Building

!!! warning "Early Development"

    HoloMUSH's building tools are actively being developed. This guide covers
    the conceptual model — how locations, exits, and objects work — so you can
    start thinking about your world. In-game building commands will be
    documented here as they become available.

Building is how game designers create the world that players explore. You don't need to write code — everything here happens through in-game commands. If you want to create plugin-powered mechanics or custom systems, see the [plugin development guide](../extending/index.md) instead.

## The World Model

HoloMUSH's world is made of three building blocks:

**Locations** are places. A tavern, a forest clearing, an airlock, a city street. Each location has a name and a description. When players use `look`, they see the location's description along with who else is there and what exits are available.

**Exits** are connections between locations. An exit has a name (like "north" or "heavy oak door"), an optional alias (like "n"), and a destination. Exits are one-way by default — if you want players to go back the way they came, you create a second exit pointing the other direction.

**Objects** are everything else — items, furniture, characters, anything that exists in a location. Every entity in the world (including locations and exits themselves) is an object underneath, identified by a unique ID.

## Writing Good Descriptions

Descriptions are the heart of a text-based world. A few things that make them work well:

**Set the scene, don't list furniture.** Instead of "There is a table. There are chairs. There is a fireplace," try "Rough-hewn tables crowd the common room, their surfaces scarred by decades of tankards and knife games. A fire crackles in a stone hearth along the far wall."

**Engage more than sight.** Mention sounds, smells, temperature, the feel of the ground underfoot. "The corridor hums with the low vibration of the station's reactor" tells players something that "A long metal corridor" doesn't.

**Keep it to a paragraph or two.** Players read descriptions frequently. A paragraph that paints a vivid picture works better than a page that gets skimmed.

**Leave room for action.** Good descriptions suggest things players can interact with without prescribing what they should do.

## Scenes

The scene system supports structured roleplay encounters. A scene has:

- **A title and description** to set up the premise
- **A participant list** so everyone knows who's involved
- **Privacy settings** to control whether others can observe

Scenes are useful when you want a clear boundary around an interaction — a private conversation in a back alley, a combat encounter, or a formal council meeting. They're optional; plenty of roleplay happens outside of scenes too.

## What's Next

Follow development on [GitHub](https://github.com/holomush/holomush) as in-game building commands are added.
