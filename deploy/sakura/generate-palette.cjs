// Run after reviewing a clean official build: node generate-palette.cjs INPUT.css SHA256
const fs = require('node:fs');
const path = require('node:path');
const crypto = require('node:crypto');
const frontend = path.resolve(__dirname, '../../frontend');
const postcss = require(require.resolve('postcss', { paths: [frontend] }));
const input = process.argv[2], expected = process.argv[3];
if (!input || !/^[a-f0-9]{64}$/.test(expected || '')) throw new Error('Provide reviewed official CSS path and SHA256');
const official = fs.readFileSync(input, 'utf8');
const hash = value => crypto.createHash('sha256').update(value).digest('hex');
if (hash(official) !== expected) throw new Error('Official CSS hash mismatch');
const primary = [
  ['f0fdfa','faf7fe'],['ccfbf1','f0eafa'],['99f6e4','e3d7f4'],
  ['5eead4','cfbbea'],['2dd4bf','b498d9'],['14b8a6','9776c8'],
  ['0d9488','8565bc'],['0f766e','6e4d9f'],['115e59','593f80'],
  ['134e4a','463263'],['042f2e','281c3c']
];
const neutrals = [
  ['f9fafb','f7f6fb'],['f3f4f6','f1eef7'],['e5e7eb','e9e4f0'],
  ['d1d5db','d7cede'],['9ca3af','9d90a8'],['6b7280','75667f'],
  ['4b5563','63566f'],['374151','50435d'],['1f2937','362d43'],['111827','27243d'],
  ['f8fafc','f7f5fb'],['f1f5f9','ede9f4'],['e2e8f0','e4ddec'],
  ['cbd5e1','cfc4dd'],['94a3b8','ad9fc0'],['64748b','83728f'],
  ['475569','675777'],['334155','493d59'],['1e293b','2c2538'],
  ['0f172a','211c2b'],['020617','17141e']
];
function recolor(value, pairs) {
  const colors = new Map(pairs);
  const rgb = new Map(pairs.map(([from,to]) => [
    [0,2,4].map(i=>parseInt(from.slice(i,i+2),16)).join(','),
    [0,2,4].map(i=>parseInt(to.slice(i,i+2),16))
  ]));
  return value.replace(/#([\da-f]{6})([\da-f]{2})?(?![\da-f])/gi, (all,hex,alpha='') =>
    colors.has(hex.toLowerCase()) ? '#'+colors.get(hex.toLowerCase())+alpha : all
  ).replace(/\b(rgb|rgba)\(\s*(\d+)(\s*,\s*|\s+)(\d+)(\s*,\s*|\s+)(\d+)/gi,
    (all,fn,r,sep1,g,sep2,b) => {
      const replacement=rgb.get([Number(r),Number(g),Number(b)].join(','));
      return replacement ? `${fn}(${replacement[0]}${sep1}${replacement[1]}${sep2}${replacement[2]}` : all;
    });
}
const source = postcss.parse(official);
const output = postcss.root();
const allowed = /^(?:color|background(?:-color|-image)?|border(?:-(?:top|bottom|left|right))?-color|outline-color|fill|stroke|box-shadow|scrollbar-color|--tw-(?:gradient-(?:from|to)|ring-color|shadow-color|shadow))$/;
const alterations=[];
source.walkRules(rule => {
  // Charts/status/platform identity colors remain as supplied by upstream.
  const usePrimary = /primary|sidebar|\.btn(?:\b|:)|\.input|\.select|\.checkbox|\.radio|\.switch|mesh-gradient|::selection/.test(rule.selector);
  const replacements = [...neutrals, ...(usePrimary ? primary : []), ...(rule.selector.includes('mesh-gradient') ? [['06b6d4','dea4bb']] : [])];
  const clone = postcss.rule({selector:rule.selector});
  rule.nodes?.forEach(decl=>{
    if(decl.type!=='decl' || !allowed.test(decl.prop)) return;
    const value = recolor(decl.value,replacements);
    if(value===decl.value) return;
    clone.append(decl.clone({value}));
    alterations.push({selector:rule.selector,property:decl.prop,before:decl.value,after:value});
  });
  if(!clone.nodes.length)return;
  let wrapped=clone;
  for(let parent=rule.parent;parent && parent.type!=='root';parent=parent.parent){
    if(parent.type!=='atrule')throw new Error('Unsupported CSS nesting');
    const wrapper=parent.clone({nodes:[]});wrapper.append(wrapped);wrapped=wrapper;
  }
  output.append(wrapped);
});
const skin='/* Sakura: color values only. Generated from reviewed official CSS; no geometry or behavior changes. */\n'+output.toString()+'\n';
postcss.parse(skin).walkDecls(d=>{if(!allowed.test(d.prop))throw new Error('Non-color property: '+d.prop)});
fs.writeFileSync(path.join(frontend,'src/styles/sakura.css'),skin);
console.log(JSON.stringify({ official_css_sha256:hash(official), skin_sha256:hash(skin), color_changes:alterations.length }));
