"use client";

import {
  Background,
  BackgroundVariant,
  Controls,
  EdgeLabelRenderer,
  getSmoothStepPath,
  Handle,
  MarkerType,
  Position,
  ReactFlow,
  type Edge,
  type EdgeProps,
  type Node,
  type NodeChange,
  type NodeProps,
  type ReactFlowInstance,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import {
  AppWindowIcon,
  CloudIcon,
  CodeXmlIcon,
  DatabaseIcon,
  ExternalLinkIcon,
  LayersIcon,
  ServerIcon,
  Settings2Icon,
  SquareIcon,
  WorkflowIcon,
  ZapIcon,
  type LucideIcon,
} from "lucide-react";
import Link from "next/link";
import { createContext, Fragment, useContext, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import {
  errorMessage,
  portUrl,
  type Agent,
  type Architecture,
  type Design,
  type DesignNode,
  type Instance,
  type NodeKind,
  type PowerName,
} from "@/lib/api";
import { useAgents } from "@/lib/agents";
import { DEFAULT_STACK, DISKS_GB, POWERS, powerName, stackLabel } from "@/lib/sizes";
import { useElementSize } from "@/lib/use-element-size";
import { cn } from "@/lib/utils";
import { StatusDot } from "../status";
import { Button } from "../ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "../ui/dropdown-menu";
import { Spinner } from "../ui/spinner";
import { TechLogo, techMark } from "./tech";

const KINDS: Record<NodeKind, { label: string; icon: LucideIcon }> = {
  frontend: { label: "Frontend", icon: AppWindowIcon },
  backend: { label: "Backend", icon: ServerIcon },
  service: { label: "Service", icon: ServerIcon },
  database: { label: "Database", icon: DatabaseIcon },
  cache: { label: "Cache", icon: ZapIcon },
  queue: { label: "Queue", icon: LayersIcon },
  ci: { label: "CI", icon: WorkflowIcon },
  external: { label: "External", icon: CloudIcon },
};

/**
 * How far along the path of a request a kind of component sits: what the
 * user sees first, then what serves it, then what holds its data.
 */
const TIER: Record<Exclude<NodeKind, "ci">, number> = {
  frontend: 0,
  backend: 1,
  service: 1,
  database: 2,
  cache: 2,
  queue: 2,
  external: 2,
};

/** Which way requests run through the diagram. */
type Direction = "across" | "down";

/** The room a component takes up when drawn. */
const NODE = { width: 240, height: 112 };

/**
 * How far apart components sit: from one tier to the next, and from one to
 * its neighbour in the same tier.
 */
const SPACING: Record<Direction, { tier: number; neighbour: number }> = {
  across: { tier: 380, neighbour: 170 },
  down: { tier: 180, neighbour: 280 },
};

/** The room left beside the path of requests for the lines to and from CI. */
const CI_GAP = 120;

/** How long a line's label can be before it is cut short, in characters. */
const LABEL_MAX = 30;

/** How fast the dot on a connection travels, in pixels a second. */
const DOT_SPEED = 70;

const FIT = { padding: 0.08, maxZoom: 1.25 };

type Point = { x: number; y: number };

/** What a node is shown with, beyond its place in the design. */
type NodeData = {
  node: DesignNode;
  /** The design has not been agreed to yet. */
  proposed: boolean;
  /** The machine it runs on, once that exists. */
  machine?: Instance;
  listening?: boolean;
  /** Agents working on its machine right now. */
  agents: Agent[];
};

type ComponentNode = Node<NodeData, "component">;

/** What a line is drawn with, beyond the two components it joins. */
type FlowData = {
  /** Requests travel along it. A line to or from CI carries none. */
  requests: boolean;
  /** The design has not been agreed to yet. */
  proposed: boolean;
  label?: string;
};

type FlowEdge = Edge<FlowData, "flow">;

type Placement = {
  position?: Point;
  measured?: { width: number; height: number };
};

/** How a proposed design's machines can be changed before agreeing to it. */
type Editing = {
  resize: (machine: string, change: { size?: PowerName; disk_gb?: number }) => void;
} | null;

const EditingContext = createContext<Editing>(null);

/**
 * Lays the components out in tiers by what they are: what the user sees,
 * then what serves it, then what holds data. The tiers run left to right or
 * top to bottom. CI is off the path requests take, beside the middle tier,
 * so its lines to the code it builds don't cross the ones requests follow.
 */
function layout(nodes: DesignNode[], direction: Direction) {
  const all: DesignNode[][] = [[], [], []];
  const ci: DesignNode[] = [];
  for (const node of nodes) {
    if (node.kind === "ci") ci.push(node);
    else all[TIER[node.kind]].push(node);
  }
  // A design with no frontend, say, shouldn't leave a gap where one would be.
  const tiers = all.filter((tier) => tier.length > 0);

  const { tier: step, neighbour } = SPACING[direction];
  const widest = Math.max(1, ...tiers.map((tier) => tier.length));
  const positions: Record<string, Point> = {};
  const place = (id: string, along: number, beside: number) => {
    positions[id] = direction === "across" ? { x: along, y: beside } : { x: beside, y: along };
  };

  tiers.forEach((tier, t) => {
    // Each tier is centred against the widest.
    const offset = ((widest - tier.length) * neighbour) / 2;
    tier.forEach((node, i) => place(node.id, t * step, offset + i * neighbour));
  });

  // Level with the middle tier, just past the last component in it.
  const middle = Math.max(0, Math.min(1, tiers.length - 1));
  const beside = tiers[middle]?.length ?? 1;
  const start = ((widest - beside) * neighbour) / 2 + (beside - 1) * neighbour;
  const size = direction === "across" ? NODE.height : NODE.width;
  const gap = direction === "across" ? CI_GAP / 2 : CI_GAP;
  ci.forEach((node, i) => place(node.id, (middle + i) * step, start + size + gap));

  return positions;
}

/** How much of its natural size a layout could be drawn at in a given room. */
function scale(positions: Record<string, Point>, room: { width: number; height: number }) {
  const points = Object.values(positions);
  if (points.length === 0) return 1;

  const width = Math.max(...points.map((p) => p.x)) + NODE.width;
  const height = Math.max(...points.map((p) => p.y)) + NODE.height;
  return Math.min(FIT.maxZoom, room.width / width, room.height / height);
}

/**
 * The sides of two components a line between them should leave and arrive
 * by, going by where they sit, so a line never starts by heading away from
 * where it is going.
 */
function sides(from: Point, to: Point): [Position, Position] {
  const dx = to.x - from.x;
  const dy = to.y - from.y;

  if (Math.abs(dx) >= Math.abs(dy)) {
    return dx >= 0 ? [Position.Right, Position.Left] : [Position.Left, Position.Right];
  }
  return dy >= 0 ? [Position.Bottom, Position.Top] : [Position.Top, Position.Bottom];
}

/**
 * The sides for a line between CI and something it builds, as [CI's, the
 * other's]. CI sits off the path requests take, and its lines stay off it
 * too: they reach a component by the side that faces CI's row or column,
 * which no request uses, so they never run along a request's line or under
 * its label.
 */
function ciSides(ci: Point, other: Point, direction: Direction): [Position, Position] {
  const dx = other.x - ci.x;
  const dy = other.y - ci.y;

  if (direction === "across") {
    const below = dy <= 0;
    const facing = below ? Position.Top : Position.Bottom;
    const level = Math.abs(dx) < NODE.width / 2;
    return [
      level ? facing : dx < 0 ? Position.Left : Position.Right,
      below ? Position.Bottom : Position.Top,
    ];
  }

  const right = dx <= 0;
  const facing = right ? Position.Left : Position.Right;
  const level = Math.abs(dy) < NODE.height / 2;
  return [
    level ? facing : dy < 0 ? Position.Top : Position.Bottom,
    right ? Position.Right : Position.Left,
  ];
}

const SIDES = [Position.Top, Position.Right, Position.Bottom, Position.Left];

/** A label short enough to sit on a line without covering what it joins. */
function shorten(label: string) {
  return label.length > LABEL_MAX ? `${label.slice(0, LABEL_MAX - 1).trimEnd()}…` : label;
}

type CanvasProps = {
  architecture: Architecture;
  /** The design to draw: the architecture's, with any changes not yet sent. */
  design: Design;
  instances: Instance[];
  agents: Agent[];
  /** Given while the design is still a proposal the user can change. */
  onResize?: (machine: string, change: { size?: PowerName; disk_gb?: number }) => void;
};

/**
 * The design as a diagram: a node for each component, a line for each
 * connection. Once built, each node shows its machine, whether it is
 * answering, and any agent at work on it; hovering one offers its editor.
 *
 * It fills whatever room it is given, and runs across or down according to
 * which shows the design larger there.
 */
export function ArchitectureCanvas({ className, ...props }: CanvasProps & { className?: string }) {
  const [ref, measured] = useElementSize<HTMLDivElement>();

  // While another tab is showing, the diagram is hidden and measures as
  // nothing. That is not a size to lay it out for: the last real one is kept,
  // so coming back finds it as it was left.
  const [room, setRoom] = useState(measured);
  if (measured && measured !== room && measured.width > 0 && measured.height > 0) {
    setRoom(measured);
  }

  const { nodes } = props.design;
  const direction = useMemo<Direction | null>(() => {
    if (!room) return null;
    const across = scale(layout(nodes, "across"), room);
    const down = scale(layout(nodes, "down"), room);
    return down > across ? "down" : "across";
  }, [nodes, room]);

  return (
    <div
      ref={ref}
      role="group"
      aria-label="Architecture diagram"
      className={cn("relative min-h-0 overflow-hidden", className)}
    >
      {/* Drawn once there is a size to draw it for. Keyed by direction:
          where a node was dragged to means nothing in the other one. */}
      {room && direction && (
        <div className="absolute inset-0">
          <Diagram key={direction} {...props} direction={direction} room={room} />
        </div>
      )}
    </div>
  );
}

function Diagram({
  architecture,
  design,
  instances,
  agents,
  onResize,
  direction,
  room,
}: CanvasProps & { direction: Direction; room: { width: number; height: number } }) {
  const proposed = architecture.status === "proposed";

  // What the diagram itself knows about each node and this component must
  // hand back to it: where the user dragged it to, and the size it measured
  // it at. A node given back without its size is treated as new, and hidden
  // until it has been measured again.
  const [placed, setPlaced] = useState<Record<string, Placement>>({});

  const positions = useMemo(() => {
    const auto = layout(design.nodes, direction);
    return Object.fromEntries(
      design.nodes.map((node) => [node.id, placed[node.id]?.position ?? auto[node.id]]),
    );
  }, [design.nodes, direction, placed]);

  const kinds = useMemo(
    () => Object.fromEntries(design.nodes.map((node) => [node.id, node.kind])),
    [design.nodes],
  );

  const nodes = useMemo<ComponentNode[]>(
    () =>
      design.nodes.map((node) => {
        const state = architecture.nodes.find((n) => n.id === node.id);
        const machine = instances.find((i) => i.name === node.machine);

        return {
          id: node.id,
          type: "component",
          position: positions[node.id],
          measured: placed[node.id]?.measured,
          data: {
            node,
            proposed,
            machine,
            listening: state?.listening,
            agents: machine
              ? agents.filter((a) => a.machine.id === machine.id && a.status === "working")
              : [],
          },
        };
      }),
    [design.nodes, architecture.nodes, instances, agents, positions, placed, proposed],
  );

  const edges = useMemo<FlowEdge[]>(
    () =>
      design.edges.flatMap((edge) => {
        const from = positions[edge.from];
        const to = positions[edge.to];
        if (!from || !to) return [];

        // Which of the two, if either, is CI decides how the line is drawn.
        const fromCI = kinds[edge.from] === "ci";
        const toCI = kinds[edge.to] === "ci";
        let [leave, arrive] = sides(from, to);
        if (fromCI && !toCI) [leave, arrive] = ciSides(from, to, direction);
        else if (toCI && !fromCI) [arrive, leave] = ciSides(to, from, direction);

        return [
          {
            id: `${edge.from}-${edge.to}`,
            source: edge.from,
            target: edge.to,
            sourceHandle: `from-${leave}`,
            targetHandle: `to-${arrive}`,
            type: "flow" as const,
            data: {
              requests: !fromCI && !toCI,
              proposed,
              label: shorten([edge.label, edge.path].filter(Boolean).join(" · ")) || undefined,
            },
            // The arrowhead takes the line's grey: fainter on a line that
            // carries no requests.
            markerEnd: {
              type: MarkerType.ArrowClosed,
              width: 16,
              height: 16,
              color: fromCI || toCI ? "rgb(255 255 255 / 0.3)" : "rgb(255 255 255 / 0.5)",
            },
          },
        ];
      }),
    [design.edges, positions, kinds, direction, proposed],
  );

  function onNodesChange(changes: NodeChange<ComponentNode>[]) {
    setPlaced((all) => {
      let next = all;
      for (const change of changes) {
        if (change.type === "position" && change.position) {
          next = { ...next, [change.id]: { ...next[change.id], position: change.position } };
        } else if (change.type === "dimensions" && change.dimensions) {
          next = { ...next, [change.id]: { ...next[change.id], measured: change.dimensions } };
        }
      }
      return next;
    });
  }

  // Fitted again whenever the room changes, so that dragging the divider or
  // resizing the window never leaves the design half out of sight.
  const flow = useRef<ReactFlowInstance<ComponentNode, FlowEdge> | null>(null);
  useEffect(() => {
    const frame = requestAnimationFrame(() => flow.current?.fitView(FIT));
    return () => cancelAnimationFrame(frame);
  }, [room.width, room.height]);

  const editing = useMemo<Editing>(
    () => (proposed && onResize ? { resize: onResize } : null),
    [proposed, onResize],
  );

  return (
    <EditingContext.Provider value={editing}>
      <ReactFlow
        // Fitted afresh when components come or go, not while one is dragged.
        key={design.nodes.map((n) => n.id).join(",")}
        nodes={nodes}
        edges={edges}
        nodeTypes={NODE_TYPES}
        edgeTypes={EDGE_TYPES}
        onNodesChange={onNodesChange}
        onInit={(instance) => {
          flow.current = instance;
        }}
        colorMode="dark"
        fitView
        fitViewOptions={FIT}
        minZoom={0.35}
        maxZoom={1.5}
        nodesConnectable={false}
        edgesFocusable={false}
        // The library's own mark in the corner is left off, as its licence allows.
        proOptions={{ hideAttribution: true }}
      >
        <Background variant={BackgroundVariant.Dots} gap={22} size={1.2} />
        <Controls showInteractive={false} position="bottom-left" />
      </ReactFlow>
    </EditingContext.Provider>
  );
}

function ComponentNodeView({ data }: NodeProps<ComponentNode>) {
  const { node, proposed, machine, listening, agents } = data;
  const editing = useContext(EditingContext);
  const { stop } = useAgents();
  const kind = KINDS[node.kind];
  const working = agents[0];
  const external = node.kind === "external";
  // Shown by what it is made with where that is recognised, and by what
  // kind of thing it is where it isn't.
  const mark = techMark(node.tech);
  const live = listening === true && machine?.status === "running";

  return (
    <div
      aria-label={`${node.name}, ${kind.label}`}
      className={cn(
        "group/node relative w-60 rounded-xl border bg-card text-card-foreground shadow-lg shadow-black/40 transition-[border-color,box-shadow] hover:border-foreground/25",
        (proposed || external) && "border-dashed shadow-none",
        // An agent is at work on it: the one node lit in the accent.
        working &&
          "border-primary/70 shadow-[0_0_28px_-6px_var(--primary)] hover:border-primary/70",
      )}
    >
      {/* A way in and a way out on every side; each line uses whichever
          faces the component at its other end. */}
      {SIDES.map((side) => (
        <Fragment key={side}>
          <Handle id={`from-${side}`} type="source" position={side} className="opacity-0!" />
          <Handle id={`to-${side}`} type="target" position={side} className="opacity-0!" />
        </Fragment>
      ))}

      <div className="flex items-start gap-3 p-3.5">
        <div className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground">
          {mark ? <TechLogo mark={mark} className="size-6" /> : <kind.icon className="size-5" />}
        </div>
        <div className="min-w-0 flex-1">
          <p className="text-xs text-muted-foreground">{kind.label}</p>
          <p className="truncate text-sm font-semibold" title={node.name}>
            {node.name}
          </p>
          <p className="truncate text-xs text-muted-foreground" title={node.tech}>
            {node.tech}
          </p>
        </div>
      </div>

      <div className="flex min-h-9 items-center gap-2 rounded-b-xl bg-black/20 px-3.5 py-2 text-xs">
        {external ? (
          <span className="text-muted-foreground">Not on your machines</span>
        ) : working ? (
          <>
            <Spinner className="size-3.5 shrink-0 text-primary" />
            <span className="min-w-0 truncate" title={working.activity}>
              <span className="font-medium">{working.handle || working.name}</span>
              <span className="text-muted-foreground">
                {working.activity ? `: ${working.activity}` : " is working"}
              </span>
            </span>
          </>
        ) : machine ? (
          <>
            <StatusDot status={machine.status} />
            <span className="min-w-0 truncate font-mono" title={machine.name} translate="no">
              {machine.name}
            </span>
            {node.port ? (
              <span
                className={cn(
                  "ml-auto shrink-0 tabular-nums",
                  live ? "text-success" : "text-muted-foreground",
                )}
              >
                {live ? `Live on :${node.port}` : `:${node.port} not up`}
              </span>
            ) : null}
          </>
        ) : (
          <>
            <span
              className="min-w-0 truncate font-mono text-muted-foreground"
              title={node.machine}
              translate="no"
            >
              {node.machine}
            </span>
            {/* What it will be made as, by the names the size menu uses,
                which leaves room for the machine's own. The stack is said
                only when it adds something: every machine has the standard
                tools. */}
            <span className="ml-auto shrink-0 text-muted-foreground tabular-nums">
              {[
                node.stack && node.stack !== DEFAULT_STACK && stackLabel(node.stack),
                powerName(node.size),
                `${node.disk_gb} GB`,
              ]
                .filter(Boolean)
                .join(" · ")}
            </span>
          </>
        )}
      </div>

      {/* What can be done with the component, shown when it is pointed at
          or focused, and always where there is no pointer to hover with. */}
      {(machine || editing) && !external && (
        <div className="nodrag absolute inset-x-2 -bottom-4 z-10 flex justify-center gap-1.5 opacity-0 transition-opacity group-focus-within/node:opacity-100 group-hover/node:opacity-100 [@media(hover:none)]:opacity-100">
          {machine && (
            <Button asChild size="xs" className="shadow-md">
              <Link href={`/instances/${machine.id}`}>
                <CodeXmlIcon data-icon="inline-start" />
                Open IDE
              </Link>
            </Button>
          )}
          {machine && node.port && listening ? (
            <PreviewButton machine={machine} port={node.port} name={node.name} />
          ) : null}
          {working && (
            <Button
              size="xs"
              variant="secondary"
              className="shadow-md"
              aria-label={`Stop ${working.handle || working.name}`}
              onClick={() => stop(working)}
            >
              <SquareIcon className="fill-current" data-icon="inline-start" />
              Stop
            </Button>
          )}
          {editing && node.machine && <ResizeMenu node={node} editing={editing} />}
        </div>
      )}
    </div>
  );
}

const NODE_TYPES = { component: ComponentNodeView };

/**
 * A connection between two components: a grey line. On one that requests
 * travel along, a small dot moves from the component that asks to the one
 * that answers; one that carries none (CI fetching code) is a thin
 * dotted line. The words on it sit over its middle.
 */
function FlowEdgeView({
  sourceX,
  sourceY,
  sourcePosition,
  targetX,
  targetY,
  targetPosition,
  markerEnd,
  data,
}: EdgeProps<FlowEdge>) {
  const [path, labelX, labelY] = getSmoothStepPath({
    sourceX,
    sourceY,
    sourcePosition,
    targetX,
    targetY,
    targetPosition,
    borderRadius: 14,
  });
  const requests = data?.requests ?? true;
  // The dot keeps one pace whatever the distance: a longer line takes it
  // longer. The line runs along the grid, so its length is near enough the
  // sum of how far across and how far down it goes.
  const length = Math.abs(targetX - sourceX) + Math.abs(targetY - sourceY);
  const travel = { animationDuration: `${Math.max(1.6, length / DOT_SPEED)}s` };
  // Not agreed to yet: the same line, at half its strength.
  const strength = data?.proposed ? 0.5 : 1;

  // Each path says for itself that it has no fill. A path is filled black
  // unless told otherwise, and one that bends then shows as a dark wedge;
  // left to the stylesheet alone, that is what a page holding an older
  // stylesheet would draw.
  return (
    <>
      {requests ? (
        <g opacity={strength}>
          <path d={path} fill="none" className="flow-line" markerEnd={markerEnd} />
          <path d={path} fill="none" pathLength={1} className="flow-dot" style={travel} />
        </g>
      ) : (
        <path d={path} fill="none" className="flow-quiet" markerEnd={markerEnd} />
      )}

      {data?.label && (
        <EdgeLabelRenderer>
          <div
            style={{ transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)` }}
            className={cn(
              "absolute rounded-md bg-stage px-1.5 py-0.5 text-[11px] leading-tight whitespace-nowrap",
              requests ? "text-muted-foreground" : "text-muted-foreground/70",
            )}
          >
            {data.label}
          </div>
        </EdgeLabelRenderer>
      )}
    </>
  );
}

const EDGE_TYPES = { flow: FlowEdgeView };

function PreviewButton({ machine, port, name }: { machine: Instance; port: number; name: string }) {
  async function open() {
    // Opened during the click, pointed at the page once its link arrives:
    // a tab opened after waiting would be blocked as a pop-up.
    const tab = window.open("", "_blank");
    try {
      const url = await portUrl(machine.id, port);
      if (tab) {
        tab.opener = null;
        tab.location.href = url;
      } else {
        window.location.href = url;
      }
    } catch (err) {
      tab?.close();
      toast.error(`Couldn't open ${name}`, { description: errorMessage(err) });
    }
  }

  return (
    <Button size="xs" variant="secondary" className="shadow-md" onClick={open}>
      <ExternalLinkIcon data-icon="inline-start" />
      Open
    </Button>
  );
}

/** Changes how powerful a proposed machine is, and how much disk it has. */
function ResizeMenu({ node, editing }: { node: DesignNode; editing: NonNullable<Editing> }) {
  const machine = node.machine!;

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button size="xs" variant="secondary" className="shadow-md">
          <Settings2Icon data-icon="inline-start" />
          Change size
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="center" className="min-w-56">
        <DropdownMenuLabel>
          Power of <span className="font-mono">{machine}</span>
        </DropdownMenuLabel>
        <DropdownMenuRadioGroup
          value={node.size}
          onValueChange={(size) => editing.resize(machine, { size: size as PowerName })}
        >
          {POWERS.map((power) => (
            <DropdownMenuRadioItem key={power.id} value={power.id}>
              {power.label}
              <span className="ml-auto pl-4 text-xs text-muted-foreground">
                {power.cpus} vCPU · {power.memoryGb} GB
              </span>
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
        <DropdownMenuSeparator />
        <DropdownMenuLabel>Storage</DropdownMenuLabel>
        <DropdownMenuRadioGroup
          value={String(node.disk_gb)}
          onValueChange={(gb) => editing.resize(machine, { disk_gb: Number(gb) })}
        >
          {DISKS_GB.map((gb) => (
            <DropdownMenuRadioItem key={gb} value={String(gb)}>
              {gb} GB
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
